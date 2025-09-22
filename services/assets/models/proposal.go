package models

type Proposal struct {
	ProposalID  uint64 `gorm:"primaryKey;autoIncrement"`
	SubmittedBy uint64
}
