package domain

// FaceUser holds the identity fields needed to build a Face-ID report row,
// extracted from a 01-edu user's free-form `attrs` object.
//
// PhotoUploadID is the storage file ID used to fetch the user's photo from the
// platform's /api/storage endpoint. It is empty when the user has no uploaded
// photo.
type FaceUser struct {
	Login         string
	LastNameCyr   string
	FirstNameCyr  string
	IIN           string
	PhotoUploadID string
}
