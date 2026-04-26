package oneedu

import "time"

// --- GraphQL response types (match the real 01-edu API schema) ---

// piscineResponse is the response for GetCurrentPiscineId.
type piscineResponse struct {
	Data struct {
		Event []piscineEventNode `json:"event"`
	} `json:"data"`
}

type piscineEventNode struct {
	ID      int       `json:"id"`
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

// raidsResponse is the response for GetRaidsByPiscine*Id and GetRaidByName.
type raidsResponse struct {
	Data struct {
		Event []raidEventNode `json:"event"`
	} `json:"data"`
}

type raidEventNode struct {
	ID       int         `json:"id"`
	Object   objectNode  `json:"object"`
	ParentID int         `json:"parentId"`
	Groups   []groupNode `json:"groups"`
	StartAt  time.Time   `json:"startAt"`
	EndAt    time.Time   `json:"endAt"`
}

type objectNode struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type groupNode struct {
	Captain     captainNode     `json:"captain"`
	GroupStatus groupStatusNode `json:"group_status"`
	Members     []memberNode    `json:"members"`
}

type captainNode struct {
	Login string `json:"login"`
}

type groupStatusNode struct {
	Status string `json:"status"`
}

type memberNode struct {
	ID        int    `json:"id"`
	UserLogin string `json:"userLogin"`
}
