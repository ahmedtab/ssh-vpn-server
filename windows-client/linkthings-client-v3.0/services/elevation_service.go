package services

// ElevationService reports the admin/root state captured once at startup
// (main.go already refuses to run at all when unelevated, so this is
// effectively always true — kept as a service for the footer badge and so
// that changes to the elevation gate don't need frontend changes).
type ElevationService struct {
	elevated bool
}

func NewElevationService(elevated bool) *ElevationService {
	return &ElevationService{elevated: elevated}
}

func (s *ElevationService) IsElevated() bool {
	return s.elevated
}
