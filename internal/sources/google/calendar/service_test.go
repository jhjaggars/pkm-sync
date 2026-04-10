package calendar

import (
	"testing"

	"google.golang.org/api/calendar/v3"
)

func TestService_shouldIncludeEvent(t *testing.T) {
	tests := []struct {
		name                     string
		attendeeAllowList        []string
		requireMultipleAttendees bool
		includeSelfOnlyEvents    bool
		event                    *calendar.Event
		expected                 bool
		description              string
	}{
		{
			name:                     "no allow list - event with multiple attendees",
			attendeeAllowList:        nil,
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com"},
					{Email: "user2@example.com"},
				},
			},
			expected:    true,
			description: "When no allow list is set, events with multiple attendees should be included",
		},
		{
			name:                     "no allow list - event with single attendee, self-only excluded",
			attendeeAllowList:        nil,
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com"},
				},
			},
			expected:    false,
			description: "When no allow list is set but self-only events are excluded, single attendee events should be excluded",
		},
		{
			name:                     "no allow list - event with single attendee, self-only included",
			attendeeAllowList:        nil,
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com"},
				},
			},
			expected:    true,
			description: "When no allow list is set and self-only events are included, single attendee events should be included",
		},
		{
			name:                     "no allow list - no attendees, self-only excluded",
			attendeeAllowList:        nil,
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{},
			},
			expected:    false,
			description: "When no allow list is set but self-only events are excluded, events with no attendees should be excluded",
		},
		{
			name:                     "no allow list - no attendees, self-only included",
			attendeeAllowList:        nil,
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{},
			},
			expected:    true,
			description: "When no allow list is set and self-only events are included, events with no attendees should be included",
		},
		{
			name:                     "allow list - matching attendee",
			attendeeAllowList:        []string{"allowed@example.com", "team@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
					{Email: "other@example.com"},
				},
			},
			expected:    true,
			description: "When allow list is set and at least one attendee matches, event should be included",
		},
		{
			name:                     "allow list - no matching attendees",
			attendeeAllowList:        []string{"allowed@example.com", "team@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "other@example.com"},
					{Email: "another@example.com"},
				},
			},
			expected:    false,
			description: "When allow list is set and no attendees match, event should be excluded",
		},
		{
			name:                     "allow list - case insensitive matching",
			attendeeAllowList:        []string{"ALLOWED@EXAMPLE.COM"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
					{Email: "other@example.com"},
				},
			},
			expected:    true,
			description: "Email matching should be case insensitive",
		},
		{
			name:                     "allow list - whitespace handling",
			attendeeAllowList:        []string{" allowed@example.com ", "team@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: " allowed@example.com "},
					{Email: "other@example.com"},
				},
			},
			expected:    true,
			description: "Email matching should handle whitespace properly",
		},
		{
			name:                     "allow list - single matching attendee passes self-only filter",
			attendeeAllowList:        []string{"allowed@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
				},
			},
			expected:    true,
			description: "Single attendee events that match allow list should pass when self-only events are included",
		},
		{
			name:                     "allow list - single matching attendee fails self-only filter",
			attendeeAllowList:        []string{"allowed@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
				},
			},
			expected:    false,
			description: "Single attendee events that match allow list should fail when self-only events are excluded",
		},
		{
			name:                     "no multiple attendees requirement - single attendee passes",
			attendeeAllowList:        []string{"allowed@example.com"},
			requireMultipleAttendees: false,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
				},
			},
			expected:    true,
			description: "When multiple attendees are not required, single attendee events should pass",
		},
		{
			name:                     "empty attendee email should be ignored",
			attendeeAllowList:        []string{"allowed@example.com"},
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: ""},
					{Email: "allowed@example.com"},
				},
			},
			expected:    true,
			description: "Attendees with empty emails should be ignored in filtering",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				attendeeAllowList:        tt.attendeeAllowList,
				requireMultipleAttendees: tt.requireMultipleAttendees,
				includeSelfOnlyEvents:    tt.includeSelfOnlyEvents,
			}

			result := service.shouldIncludeEvent(tt.event)
			if result != tt.expected {
				t.Errorf("shouldIncludeEvent() = %v, expected %v. %s", result, tt.expected, tt.description)
			}
		})
	}
}

func TestService_passesAttendeeAllowListFilter(t *testing.T) {
	tests := []struct {
		name              string
		attendeeAllowList []string
		event             *calendar.Event
		expected          bool
		description       string
	}{
		{
			name:              "empty allow list - should pass",
			attendeeAllowList: []string{},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "anyone@example.com"},
				},
			},
			expected:    true,
			description: "When allow list is empty, all events should pass",
		},
		{
			name:              "nil allow list - should pass",
			attendeeAllowList: nil,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "anyone@example.com"},
				},
			},
			expected:    true,
			description: "When allow list is nil, all events should pass",
		},
		{
			name:              "matching email in allow list",
			attendeeAllowList: []string{"allowed@example.com", "team@company.com"},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
					{Email: "other@example.com"},
				},
			},
			expected:    true,
			description: "When at least one attendee matches allow list, should pass",
		},
		{
			name:              "no matching emails in allow list",
			attendeeAllowList: []string{"allowed@example.com", "team@company.com"},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "notallowed@example.com"},
					{Email: "other@example.com"},
				},
			},
			expected:    false,
			description: "When no attendees match allow list, should fail",
		},
		{
			name:              "case insensitive matching",
			attendeeAllowList: []string{"ALLOWED@EXAMPLE.COM"},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "allowed@example.com"},
				},
			},
			expected:    true,
			description: "Email matching should be case insensitive",
		},
		{
			name:              "whitespace trimming",
			attendeeAllowList: []string{" allowed@example.com "},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: " allowed@example.com "},
				},
			},
			expected:    true,
			description: "Whitespace should be trimmed from both allow list and attendee emails",
		},
		{
			name:              "empty attendee email ignored",
			attendeeAllowList: []string{"allowed@example.com"},
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: ""},
					{Email: "notallowed@example.com"},
				},
			},
			expected:    false,
			description: "Empty attendee emails should be ignored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				attendeeAllowList: tt.attendeeAllowList,
			}

			result := service.passesAttendeeAllowListFilter(tt.event)
			if result != tt.expected {
				t.Errorf("passesAttendeeAllowListFilter() = %v, expected %v. %s", result, tt.expected, tt.description)
			}
		})
	}
}

func TestService_passesSelfOnlyEventFilter(t *testing.T) {
	tests := []struct {
		name                     string
		requireMultipleAttendees bool
		includeSelfOnlyEvents    bool
		event                    *calendar.Event
		expected                 bool
		description              string
	}{
		{
			name:                     "require multiple attendees disabled - should always pass",
			requireMultipleAttendees: false,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{},
			},
			expected:    true,
			description: "When multiple attendees are not required, all events should pass",
		},
		{
			name:                     "zero attendees - self-only events included",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{},
			},
			expected:    true,
			description: "Events with zero attendees should pass when self-only events are included",
		},
		{
			name:                     "zero attendees - self-only events excluded",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{},
			},
			expected:    false,
			description: "Events with zero attendees should fail when self-only events are excluded",
		},
		{
			name:                     "one attendee - self-only events included",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    true,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user@example.com"},
				},
			},
			expected:    true,
			description: "Events with one attendee should pass when self-only events are included",
		},
		{
			name:                     "one attendee - self-only events excluded",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user@example.com"},
				},
			},
			expected:    false,
			description: "Events with one attendee should fail when self-only events are excluded",
		},
		{
			name:                     "two attendees - should always pass",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com"},
					{Email: "user2@example.com"},
				},
			},
			expected:    true,
			description: "Events with two or more attendees should always pass",
		},
		{
			name:                     "three attendees - should always pass",
			requireMultipleAttendees: true,
			includeSelfOnlyEvents:    false,
			event: &calendar.Event{
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com"},
					{Email: "user2@example.com"},
					{Email: "user3@example.com"},
				},
			},
			expected:    true,
			description: "Events with three or more attendees should always pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				requireMultipleAttendees: tt.requireMultipleAttendees,
				includeSelfOnlyEvents:    tt.includeSelfOnlyEvents,
			}

			result := service.passesSelfOnlyEventFilter(tt.event)
			if result != tt.expected {
				t.Errorf("passesSelfOnlyEventFilter() = %v, expected %v. %s", result, tt.expected, tt.description)
			}
		})
	}
}

func TestService_filterEvents(t *testing.T) {
	service := &Service{
		attendeeAllowList:        []string{"allowed@example.com"},
		requireMultipleAttendees: true,
		includeSelfOnlyEvents:    false,
	}

	inputEvents := []*calendar.Event{
		// Should be included: matches allow list and has multiple attendees
		{
			Attendees: []*calendar.EventAttendee{
				{Email: "allowed@example.com"},
				{Email: "other@example.com"},
			},
		},
		// Should be excluded: doesn't match allow list
		{
			Attendees: []*calendar.EventAttendee{
				{Email: "notallowed@example.com"},
				{Email: "other@example.com"},
			},
		},
		// Should be excluded: matches allow list but is self-only event
		{
			Attendees: []*calendar.EventAttendee{
				{Email: "allowed@example.com"},
			},
		},
		// Should be excluded: doesn't match allow list and is self-only event
		{
			Attendees: []*calendar.EventAttendee{
				{Email: "notallowed@example.com"},
			},
		},
	}

	result := service.filterEvents(inputEvents)

	if len(result) != 1 {
		t.Errorf("filterEvents() returned %d events, expected 1", len(result))
	}

	// Verify the correct event was included
	if len(result) > 0 {
		expectedEmail := "allowed@example.com"
		if len(result[0].Attendees) < 1 || result[0].Attendees[0].Email != expectedEmail {
			t.Errorf("filterEvents() returned wrong event, expected first attendee email to be %s", expectedEmail)
		}

		if len(result[0].Attendees) != 2 {
			t.Errorf("filterEvents() returned event with %d attendees, expected 2", len(result[0].Attendees))
		}
	}
}

func TestService_SetAttendeeAllowList(t *testing.T) {
	service := &Service{}

	allowList := []string{"user1@example.com", "user2@example.com"}
	service.SetAttendeeAllowList(allowList)

	if len(service.attendeeAllowList) != 2 {
		t.Errorf("SetAttendeeAllowList() set %d items, expected 2", len(service.attendeeAllowList))
	}

	if service.attendeeAllowList[0] != "user1@example.com" {
		t.Errorf("SetAttendeeAllowList() first item = %s, expected user1@example.com", service.attendeeAllowList[0])
	}
}

func TestService_SetRequireMultipleAttendees(t *testing.T) {
	service := &Service{}

	// Test setting to false
	service.SetRequireMultipleAttendees(false)

	if service.requireMultipleAttendees != false {
		t.Errorf("SetRequireMultipleAttendees(false) = %v, expected false", service.requireMultipleAttendees)
	}

	// Test setting to true
	service.SetRequireMultipleAttendees(true)

	if service.requireMultipleAttendees != true {
		t.Errorf("SetRequireMultipleAttendees(true) = %v, expected true", service.requireMultipleAttendees)
	}
}

func TestService_ConvertToModel_MyResponseStatus(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name               string
		event              *calendar.Event
		wantResponseStatus string
		wantAttendeeCount  int
	}{
		{
			name: "self attendee with accepted status",
			event: &calendar.Event{
				Id:      "evt-1",
				Summary: "Team meeting",
				Start:   &calendar.EventDateTime{DateTime: "2024-06-01T10:00:00Z"},
				End:     &calendar.EventDateTime{DateTime: "2024-06-01T11:00:00Z"},
				Attendees: []*calendar.EventAttendee{
					{Email: "me@example.com", Self: true, ResponseStatus: "accepted"},
					{Email: "other@example.com", ResponseStatus: "tentative"},
				},
			},
			wantResponseStatus: "accepted",
			wantAttendeeCount:  2,
		},
		{
			name: "self attendee declined",
			event: &calendar.Event{
				Id:      "evt-2",
				Summary: "Skipped meeting",
				Start:   &calendar.EventDateTime{DateTime: "2024-06-01T10:00:00Z"},
				End:     &calendar.EventDateTime{DateTime: "2024-06-01T11:00:00Z"},
				Attendees: []*calendar.EventAttendee{
					{Email: "other@example.com", ResponseStatus: "accepted"},
					{Email: "me@example.com", Self: true, ResponseStatus: "declined"},
				},
			},
			wantResponseStatus: "declined",
			wantAttendeeCount:  2,
		},
		{
			name: "no self attendee",
			event: &calendar.Event{
				Id:      "evt-3",
				Summary: "Other meeting",
				Start:   &calendar.EventDateTime{DateTime: "2024-06-01T10:00:00Z"},
				End:     &calendar.EventDateTime{DateTime: "2024-06-01T11:00:00Z"},
				Attendees: []*calendar.EventAttendee{
					{Email: "user1@example.com", ResponseStatus: "accepted"},
					{Email: "user2@example.com", ResponseStatus: "accepted"},
				},
			},
			wantResponseStatus: "",
			wantAttendeeCount:  2,
		},
		{
			name: "no attendees at all",
			event: &calendar.Event{
				Id:      "evt-4",
				Summary: "Solo event",
				Start:   &calendar.EventDateTime{DateTime: "2024-06-01T10:00:00Z"},
				End:     &calendar.EventDateTime{DateTime: "2024-06-01T11:00:00Z"},
			},
			wantResponseStatus: "",
			wantAttendeeCount:  0,
		},
		{
			name: "self attendee with empty email still sets response status",
			event: &calendar.Event{
				Id:      "evt-5",
				Summary: "Edge case meeting",
				Start:   &calendar.EventDateTime{DateTime: "2024-06-01T10:00:00Z"},
				End:     &calendar.EventDateTime{DateTime: "2024-06-01T11:00:00Z"},
				Attendees: []*calendar.EventAttendee{
					{Email: "", Self: true, ResponseStatus: "accepted"},
					{Email: "other@example.com", ResponseStatus: "tentative"},
				},
			},
			wantResponseStatus: "accepted",
			wantAttendeeCount:  1, // empty-email attendee not added to model
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := service.ConvertToModel(tt.event)

			if model.MyResponseStatus != tt.wantResponseStatus {
				t.Errorf("MyResponseStatus = %q, want %q", model.MyResponseStatus, tt.wantResponseStatus)
			}

			if len(model.Attendees) != tt.wantAttendeeCount {
				t.Errorf("Attendee count = %d, want %d", len(model.Attendees), tt.wantAttendeeCount)
			}
		})
	}
}

func TestService_SetIncludeSelfOnlyEvents(t *testing.T) {
	service := &Service{}

	// Test setting to true
	service.SetIncludeSelfOnlyEvents(true)

	if service.includeSelfOnlyEvents != true {
		t.Errorf("SetIncludeSelfOnlyEvents(true) = %v, expected true", service.includeSelfOnlyEvents)
	}

	// Test setting to false
	service.SetIncludeSelfOnlyEvents(false)

	if service.includeSelfOnlyEvents != false {
		t.Errorf("SetIncludeSelfOnlyEvents(false) = %v, expected false", service.includeSelfOnlyEvents)
	}
}
