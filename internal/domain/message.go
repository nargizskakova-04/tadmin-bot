package domain

// MessageType identifies the kind of scheduled announcement.
type MessageType string

const (
	// MsgFAQ — week 1, Monday 10:00 (admin chat).
	MsgFAQ MessageType = "faq"

	// MsgExamAnnouncement — weeks 1-3, Thursday 14:30.
	MsgExamAnnouncement MessageType = "exam_announcement"

	// MsgHackathon — week 3, Friday 10:00 (Go & JS only).
	MsgHackathon MessageType = "hackathon"

	// MsgDefenseReminder — weeks 1-3, Sunday 15:00 (admin chat).
	MsgDefenseReminder MessageType = "defense_reminder"

	// MsgStudentMessage — weeks 1-3, Sunday 15:00 (for students).
	MsgStudentMessage MessageType = "student_message"

	// MsgFinalExam — last week, Thursday 14:00.
	MsgFinalExam MessageType = "final_exam"
)
