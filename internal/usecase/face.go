package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"admin-bot/internal/domain"
)

// MaxFaceLogins bounds how many logins a single /face-scripts request may
// process, so one command can't fan out into an unbounded number of upstream
// calls.
const MaxFaceLogins = 200

// FaceUseCase collects the per-login identity data and photos that make up a
// Face-ID report.
type FaceUseCase struct {
	eduClient domain.OneEduClient
	logger    *slog.Logger
}

// NewFaceUseCase constructs a FaceUseCase.
func NewFaceUseCase(eduClient domain.OneEduClient, logger *slog.Logger) *FaceUseCase {
	return &FaceUseCase{eduClient: eduClient, logger: logger}
}

// FaceRecord is a single resolved login: either user data (optionally with a
// photo) or a failure flag explaining why the row is empty.
type FaceRecord struct {
	Index     int    // 1-based position in the report
	Login     string
	LastName  string
	FirstName string
	IIN       string

	PhotoName string // file name inside the zip / referenced by the table; "" if none
	Photo     []byte // raw image bytes; nil if none

	NotFound bool   // login did not resolve to a user
	Err      string // non-empty when lookup/download failed
}

// Collect resolves every login into a FaceRecord, downloading photos where
// available. It never fails the whole batch on a single bad login: per-login
// errors are recorded on the record and the report continues.
func (uc *FaceUseCase) Collect(ctx context.Context, logins []string) []FaceRecord {
	records := make([]FaceRecord, 0, len(logins))

	for i, login := range logins {
		rec := FaceRecord{Index: i + 1, Login: login}

		user, err := uc.eduClient.GetUserByLogin(ctx, login)
		if err != nil {
			uc.logger.Warn("face: get user failed", "login", login, "err", err)
			rec.Err = "не удалось получить данные пользователя"
			records = append(records, rec)
			continue
		}
		if user == nil {
			rec.NotFound = true
			records = append(records, rec)
			continue
		}

		rec.LastName = user.LastNameCyr
		rec.FirstName = user.FirstNameCyr
		rec.IIN = user.IIN

		if user.PhotoUploadID != "" {
			data, ext, err := uc.eduClient.DownloadPhoto(ctx, user.PhotoUploadID)
			if err != nil {
				uc.logger.Warn("face: download photo failed", "login", login, "err", err)
			} else {
				rec.Photo = data
				rec.PhotoName = photoFileName(rec, ext)
			}
		}

		records = append(records, rec)
	}

	return records
}

// photoFileName builds a stable, unique, filesystem-safe name for a record's
// photo. The index prefix guarantees uniqueness even when two users share a
// name or a login has no name at all.
func photoFileName(rec FaceRecord, ext string) string {
	base := sanitizeFileName(fmt.Sprintf("%s %s", rec.LastName, rec.FirstName))
	if base == "" {
		base = sanitizeFileName(rec.Login)
	}
	if base == "" {
		base = "user"
	}
	return fmt.Sprintf("%02d_%s%s", rec.Index, base, ext)
}

// sanitizeFileName keeps letters, digits, spaces, hyphens and underscores
// (mirroring the reference script) and collapses the result, so the name is
// safe inside a zip archive on any platform.
func sanitizeFileName(s string) string {
	var b []rune
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b = append(b, r)
		case r == ' ' || r == '-' || r == '_':
			b = append(b, r)
		case r >= 0x0400 && r <= 0x04FF: // Cyrillic — names are Cyrillic in attrs
			b = append(b, r)
		}
	}
	// Trim leading/trailing spaces.
	out := string(b)
	for len(out) > 0 && out[0] == ' ' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}
