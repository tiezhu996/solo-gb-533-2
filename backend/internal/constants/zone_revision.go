package constants

const (
	RevisionStatusDraft     = "draft"
	RevisionStatusPublished = "published"
)

func ValidRevisionStatus(value string) bool {
	switch value {
	case RevisionStatusDraft, RevisionStatusPublished:
		return true
	default:
		return false
	}
}
