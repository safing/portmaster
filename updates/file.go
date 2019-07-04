package updates

// File represents a file from the update system.
type File struct {
	filepath string
	version  string
	stable   bool
}

// NewFile combines update file attributes into an easy to use object.
func NewFile(filepath string, version string, stable bool) *File {
	return &File{
		filepath: filepath,
		version:  version,
		stable:   stable,
	}
}

// Path returns the filepath of the file.
func (f *File) Path() string {
	return f.filepath
}

// Version returns the version of the file.
func (f *File) Version() string {
	return f.version
}

// Stable returns whether the file is from a stable release.
func (f *File) Stable() bool {
	return f.stable
}

// Open opens the file and returns the
func (f *File) Open() {

}

// ReportError reports an error back to Safing. This will not automatically blacklist the file.
func (f *File) ReportError() {

}

// Blacklist notifies the update system that this file is somehow broken, and should be ignored from now on.
func (f *File) Blacklist() {

}
