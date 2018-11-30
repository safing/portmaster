package profile

var (
	fingerprintWeights = map[string]int{
		"full_path":    2,
		"partial_path": 1,
		"md5_sum":      4,
		"sha1_sum":     5,
		"sha256_sum":   6,
	}
)

type Fingerprint struct {
	OS      string
	Type    string
	Value   string
	Comment string
}

func (fp *Fingerprint) MatchesOS() bool {
	return fp.OS == osIdentifier
}

//
// func (fp *Fingerprint) Equals(other *Fingerprint) bool {
//   return fp.OS == other.OS &&
//     fp.Type == other.Type &&
//     fp.Value == other.Value
// }
//
// func (fp *Fingerprint) Check(type, value string) (weight int) {
//   if fp.Match(fpType, value) {
//     return GetFingerprintWeight(fpType)
//   }
//   return 0
// }
//
// func (fp *Fingerprint) Match(fpType, value string) (matches bool) {
//   switch fp.Type {
//   case "partial_path":
//     return
//   default:
//   return fp.OS == osIdentifier &&
//     fp.Type == fpType &&
//     fp.Value == value
// }
//
func GetFingerprintWeight(fpType string) (weight int) {
	weight, ok := fingerprintWeights[fpType]
	if ok {
		return weight
	}
	return 0
}

//
// func (p *Profile) GetApplicableFingerprints() (fingerprints []*Fingerprint) {
//   for _, fp := range p.Fingerprints {
//     if fp.OS == osIdentifier {
//       fingerprints = append(fingerprints, fp)
//     }
//   }
//   return
// }
//
// func (p *Profile) AddFingerprint(fp *Fingerprint) error {
//   if fp.OS == "" {
//     fp.OS = osIdentifier
//   }
//
//   p.Fingerprints = append(p.Fingerprints, fp)
//   return p.Save()
// }
//
// func (p *Profile) GetApplicableFingerprintTypes() (types []string) {
//   for _, fp := range p.Fingerprints {
//     if fp.OS == osIdentifier && !utils.StringInSlice(types, fp.Type) {
//       types = append(types, fp.Type)
//     }
//   }
//   return
// }
//
// func (p *Profile) MatchFingerprints(fingerprints map[string]string) (score int) {
//   for _, fp := range p.Fingerprints {
//     if fp.OS == osIdentifier {
//
//     }
//   }
//   return
// }
//
// func FindUserProfiles() {
//
// }
//
// func FindProfiles(path string) (*ProfileSet, error) {
//
// }
