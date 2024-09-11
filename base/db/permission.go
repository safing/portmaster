package db

const (
	// PermitSelf declares that the record may only be accessed by the
	// software itself and its own (first party) components.
	PermitSelf int8 = 0

	// PermitAdmin declares that the record may only be accessed by authenticated
	// third party applications that are categorized as representing an
	// administrator and has broad in access.
	PermitAdmin int8 = 1

	// PermitUser declares that the record may only be accessed by authenticated
	// third party applications that are categorized as representing a simple
	// user and is limited in access.
	PermitUser int8 = 2

	// PermitAnyone declares that anyone can access a record.
	PermitAnyone int8 = 3
)

func CheckPermission(accessPermission, recordPermission int8) bool {
	switch {
	case accessPermission < 0:
		// Negative permissions are reserved for special cases.
		return false
	case recordPermission < 0:
		// Negative permissions are reserved for special cases.
		return false
	default:
		return accessPermission <= recordPermission
	}
}
