package tags

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/shlex"

	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func init() {
	if err := process.RegisterTagHandler(new(InterpHandler)); err != nil {
		panic(err)
	}
}

type interpType struct {
	process.TagDescription

	Extensions []string
	Regex      *regexp.Regexp
}

var knownInterperters = []interpType{
	{
		TagDescription: process.TagDescription{
			ID:   "python-script",
			Name: "Python Script",
		},
		Extensions: []string{".py", ".py2", ".py3"},
		Regex:      regexp.MustCompile(`^(/usr)?/bin/python[23](\.[0-9]+)?$`),
	},
	{
		TagDescription: process.TagDescription{
			ID:   "shell-script",
			Name: "Shell Script",
		},
		Extensions: []string{".sh", ".bash", ".ksh", ".zsh", ".ash"},
		Regex:      regexp.MustCompile(`^(/usr)?/bin/(ba|k|z|a)?sh$`),
	},
	{
		TagDescription: process.TagDescription{
			ID:   "perl-script",
			Name: "Perl Script",
		},
		Extensions: []string{".pl"},
		Regex:      regexp.MustCompile(`^(/usr)?/bin/perl$`),
	},
	{
		TagDescription: process.TagDescription{
			ID:   "ruby-script",
			Name: "Ruby Script",
		},
		Extensions: []string{".rb"},
		Regex:      regexp.MustCompile(`^(/usr)?/bin/ruby$`),
	},
	{
		TagDescription: process.TagDescription{
			ID:   "nodejs-script",
			Name: "NodeJS Script",
		},
		Extensions: []string{".js"},
		Regex:      regexp.MustCompile(`^(/usr)?/bin/node(js)?$`),
	},
	/*
	   While similar to nodejs, electron is a bit harder as it uses a multiple processes
	   like Chromium and thus a interpreter match on them will but those processes into
	   different groups.

	   I'm still not sure how this could work in the future. Maybe processes should try to
	   inherit the profile of the parents if there is no profile that matches the current one....

	   	{
	   		TagDescription: process.TagDescription{
	   			ID:   "electron-app",
	   			Name: "Electron App",
	   		},
	   		Regex: regexp.MustCompile(`^(/usr)?/bin/electron([0-9]+)?$`),
	   	},
	*/
}

func fileMustBeUTF8(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer func() {
		_ = f.Close()
	}()

	// read the first chunk of bytes
	buf := new(bytes.Buffer)
	size, _ := io.CopyN(buf, f, 128)
	if size == 0 {
		return false
	}

	b := buf.Bytes()[:size]
	for len(b) > 0 {
		r, runeSize := utf8.DecodeRune(b)
		if r == utf8.RuneError {
			return false
		}

		b = b[runeSize:]
	}

	return true
}

// InterpHandler supports adding process tags based on well-known interpreter binaries.
type InterpHandler struct{}

// Name returns "Interpreter".
func (h *InterpHandler) Name() string {
	return "Interpreter"
}

// TagDescriptions returns a set of tag descriptions that InterpHandler provides.
func (h *InterpHandler) TagDescriptions() []process.TagDescription {
	l := make([]process.TagDescription, len(knownInterperters))
	for idx, it := range knownInterperters {
		l[idx] = it.TagDescription
	}

	return l
}

// CreateProfile creates a new profile for any process that has a tag created
// by InterpHandler.
func (h *InterpHandler) CreateProfile(p *process.Process) *profile.Profile {
	for _, it := range knownInterperters {
		if tag, ok := p.GetTag(it.ID); ok {
			// we can safely ignore the error
			args, err := shlex.Split(p.CmdLine)
			if err != nil {
				// this should not happen since we already called shlex.Split()
				// when adding the tag. Though, make the linter happy and bail out
				return nil
			}

			// if arg0 is the interpreter name itself strip it away
			// and use the next one
			if it.Regex.MatchString(args[0]) && len(args) > 1 {
				args = args[1:]
			}

			// Create a nice script name from filename.
			scriptName := filepath.Base(args[0])
			for _, ext := range it.Extensions {
				scriptName, _ = strings.CutSuffix(scriptName, ext)
			}
			scriptName = binmeta.GenerateBinaryNameFromPath(scriptName)

			return profile.New(&profile.Profile{
				Source:              profile.SourceLocal,
				Name:                fmt.Sprintf("%s: %s", it.Name, scriptName),
				PresentationPath:    tag.Value,
				UsePresentationPath: true,
				Fingerprints: []profile.Fingerprint{
					{
						Type:      profile.FingerprintTypeTagID,
						Key:       it.ID,
						Operation: profile.FingerprintOperationEqualsID,
						Value:     tag.Value,
					},
				},
			})
		}
	}
	return nil
}

// AddTags inspects the process p and adds any interpreter tags that InterpHandler
// detects.
func (h *InterpHandler) AddTags(p *process.Process) {
	// check if we have a matching interpreter
	var matched interpType
	for _, it := range knownInterperters {
		if it.Regex.MatchString(p.Path) {
			matched = it
		}
	}

	// zero value means we did not find any interpreter matches.
	if matched.ID == "" {
		return
	}

	args, err := shlex.Split(p.CmdLine)
	if err != nil {
		// give up if we failed to parse the command line
		return
	}

	// if args[0] matches the interpreter name we expect
	// the second arg to be a file-name
	if matched.Regex.MatchString(args[0]) {
		if len(args) == 1 {
			// there's no argument given, this is likely an interactive
			// interpreter session
			return
		}

		scriptPath := args[1]
		if !filepath.IsAbs(scriptPath) {
			scriptPath = filepath.Join(
				p.Cwd,
				scriptPath,
			)
		}

		// TODO(ppacher): there could be some other arguments as well
		// so it may be better to scan the whole command line for a path to a UTF8
		// file and use that one.
		if !fileMustBeUTF8(scriptPath) {
			return
		}

		p.Tags = append(p.Tags, profile.Tag{
			Key:   matched.ID,
			Value: scriptPath,
		})

		p.MatchingPath = scriptPath

		return
	}

	// we know that this process is interpreted by some known interpreter but args[0]
	// does not contain the path to the interpreter.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   matched.ID,
		Value: args[0],
	})

	p.MatchingPath = args[0]
}
