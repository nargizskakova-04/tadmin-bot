package domain

type AstanaUpdatesInfo struct {
	Total     int
	Succeeded int
	Checkin   int
	Piscinego int
}

type RegionUpdatesInfo struct {
	Region                    string
	SignedUpWithoutOnboarding int
	SucceededOnboardingGames  int
	CheckinRegistrations      int
	PiscineGoRegistrations    int
	CoreUsers                 int
}

type RegionUpdatesError struct {
	Region string
	Err    error
}

type RegionUpdatesReport struct {
	Regions []RegionUpdatesInfo
	Errors  []RegionUpdatesError
}
