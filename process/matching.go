package process

import (
	"context"
	"fmt"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

var (
	profileDB = database.NewInterface(nil)
)

// FindProfiles finds and assigns a profile set to the process.
func (p *Process) FindProfiles(ctx context.Context) error {
	log.Tracer(ctx).Trace("process: loading profile set")

	p.Lock()
	defer p.Unlock()

	// only find profiles if not already done.
	if p.profileSet != nil {
		return nil
	}

	// User Profile
	it, err := profileDB.Query(query.New(profile.MakeProfileKey(profile.UserNamespace, "")).Where(query.Where("LinkedPath", query.SameAs, p.Path)))
	if err != nil {
		return err
	}

	var userProfile *profile.Profile
	// get first result
	r := <-it.Next
	// cancel immediately
	it.Cancel()
	// ensure its a profile
	userProfile, err = profile.EnsureProfile(r)
	if err != nil {
		return err
	}

	// create new profile if it does not exist.
	if userProfile == nil {
		// create new profile
		userProfile = profile.New()
		userProfile.Name = p.ExecName
		userProfile.LinkedPath = p.Path
	}

	if userProfile.MarkUsed() {
		_ = userProfile.Save(profile.UserNamespace)
	}

	// Stamp
	// Find/Re-evaluate Stamp profile
	// 1. check linked stamp profile
	// 2. if last check is was more than a week ago, fetch from stamp:
	// 3. send path identifier to stamp
	// 4. evaluate all returned profiles
	// 5. select best
	// 6. link stamp profile to user profile
	// FIXME: implement!

	p.UserProfileKey = userProfile.Key()
	p.profileSet = profile.NewSet(ctx, fmt.Sprintf("%d-%s", p.Pid, p.Path), userProfile, nil)
	go p.Save()

	return nil
}

//nolint:deadcode,unused // FIXME
func matchProfile(p *Process, prof *profile.Profile) (score int) {
	for _, fp := range prof.Fingerprints {
		score += matchFingerprint(p, fp)
	}
	return
}

//nolint:deadcode,unused // FIXME
func matchFingerprint(p *Process, fp *profile.Fingerprint) (score int) {
	if !fp.MatchesOS() {
		return 0
	}

	switch fp.Type {
	case "full_path":
		if p.Path == fp.Value {
			return profile.GetFingerprintWeight(fp.Type)
		}
	case "partial_path":
		// FIXME: if full_path matches, do not match partial paths
		return profile.GetFingerprintWeight(fp.Type)
	case "md5_sum", "sha1_sum", "sha256_sum":
		// FIXME: one sum is enough, check sums in a grouped form, start with the best
		sum, err := p.GetExecHash(fp.Type)
		if err != nil {
			log.Errorf("process: failed to get hash of executable: %s", err)
		} else if sum == fp.Value {
			return profile.GetFingerprintWeight(fp.Type)
		}
	}

	return 0
}
