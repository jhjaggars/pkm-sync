package models

import "time"

type Attendee struct {
	Email          string
	DisplayName    string
	ResponseStatus string // "accepted", "declined", "tentative", "needsAction"
	Self           bool   // true if this attendee is the calendar owner
}

// GetDisplayName returns the display name if available, otherwise returns email.
func (a *Attendee) GetDisplayName() string {
	if a.DisplayName != "" {
		return a.DisplayName
	}

	return a.Email
}

type CalendarEvent struct {
	ID               string
	Summary          string
	Description      string
	Start            time.Time
	End              time.Time
	StartTime        time.Time
	EndTime          time.Time
	IsAllDay         bool
	Location         string
	Attendees        []Attendee
	MyResponseStatus string // The calendar owner's response: "accepted", "declined", "tentative", "needsAction"
	MeetingURL       string
	Attachments      []CalendarAttachment
}

type CalendarAttachment struct {
	FileURL  string
	FileID   string
	Title    string
	MimeType string
	IconLink string
}

type DriveFile struct {
	ID           string
	Name         string
	MimeType     string
	WebViewLink  string
	ModifiedTime time.Time
	Owners       []string
	Shared       bool
}
