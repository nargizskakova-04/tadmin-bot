package domain

import "errors"

var (
	// ErrPiscineNotFound is returned when an unknown PiscineType is requested.
	ErrPiscineNotFound = errors.New("piscine type not found")

	// ErrTemplateNotFound is returned when a template key has no matching file.
	ErrTemplateNotFound = errors.New("template not found")

	// ErrTokenRefresh is returned when the 01-edu token refresh fails.
	ErrTokenRefresh = errors.New("failed to refresh 01-edu token")

	// ErrGraphQL is returned when a GraphQL query fails.
	ErrGraphQL = errors.New("graphql query error")
)
