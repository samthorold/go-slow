package editor

import (
	"time"

	"github.com/samthorold/go-slow/internal/interest"
)

// ToBrief is a Story the Owner's Topics select, with the Developments it has
// had in a window: what the Sunday read is shown of the week, until it reads
// Spans (and what the earlier Briefing, over Stories, was shown of a day).
type ToBrief struct {
	Story Story
	// Today are the Developments in the window, in order.
	Today []Development
	// Topics are the names of the Topics whose Items the Developments came
	// from, and Depth the closest Depth among them.
	Topics []string
	Depth  interest.Depth
}

// StoryBriefing is a Briefing in its earlier shape: the Stories that moved
// on, each told in statements citing the Developments they rest on. The
// Record keeps those it wrote, and their days' pages still show them; none
// is written now (the Briefing tells Spans).
type StoryBriefing struct {
	Day       string
	WrittenAt time.Time
	Stories   []BriefedStory
	Model     string
}

// BriefedStory is one Story as a StoryBriefing tells it.
type BriefedStory struct {
	Story      Story
	Statements []StoryStatement
}

// StoryStatement is one statement of a StoryBriefing, with the Developments
// it rests on.
type StoryStatement struct {
	Text         string
	Developments []Development
}

// BriefingCitation is a Development a StoryBriefing cites, as it was
// written, with the kept calls behind it: the placing call that made it a
// Development (in Development.PlacingCall) and ReadingCall, the reading call
// that drew its Fact; each zero if none was kept.
type BriefingCitation struct {
	StoryID     int64
	StoryTitle  string
	Development Development
	ReadingCall int64
}

// ToPlace is an Item's Facts awaiting placing. They are placed together, in
// one call, so Facts from one Item can start one new Story between them.
type ToPlace struct {
	Item  Item
	Facts []Fact
	// Mentions are the Entities the Item mentions, as its reading gave
	// them, which decide what it is weighed against.
	Mentions []Mention
	// Depth is the closest Depth of the Topics tying the Item now; zero if
	// none does.
	Depth interest.Depth
	// Precedents are the Owner's past Feedback on Developments that share
	// an Entity or a Topic with the Facts.
	Precedents []Precedent
}

// Story is an ongoing subject, made of its Developments. Entities are the
// names of every Entity its Developments involve, in any role.
type Story struct {
	ID       int64
	Title    string
	Entities []string
	// Shares, on a Story given as a candidate for placing, are the names of
	// the Entities its Items mention that the Item being placed mentions
	// too: why it is a candidate.
	Shares []string
	// Timeline is the Story's Developments in order.
	Timeline []Development
	// Unwanted is set once the Owner said it should not be in the
	// Briefing, and Missed when the Owner started it, saying it was missed.
	Unwanted, Missed bool
	// Causes are the cause Connections to or from its Developments.
	Causes []CauseConnection
}

// StoryConnection is another Story connected to a Story, and why. It is
// derived from what the Stories share, never stored (system design:
// Connections are made between Facts and derived between Stories).
type StoryConnection struct {
	ID    int64
	Title string
	// Shared are the Developments the two Stories share.
	Shared []Development
	// Items are the Items both Stories have Developments from: announced
	// together in them. An Item reached only through a shared Development
	// is not repeated here.
	Items []SharedItem
	// Entities are the Entities their Facts, drawn from different Items,
	// involve in the same role, the rarest over Stories first.
	Entities []SharedEntity
}

// SharedItem is an Item two Stories have Developments from.
type SharedItem struct {
	SourceID string
	EntryID  string
	Title    string
}

// SharedEntity is an Entity that Facts in two Stories involve in the same
// role: Here is a Fact in the Story, There one in the Story connected to it.
type SharedEntity struct {
	Entity string
	Role   string
	// Stories is how many Stories' Facts involve the Entity: the fewer, the
	// stronger the tie.
	Stories int
	Here    Development
	There   Development
}

// CauseConnection is a cause Connection as the Record holds it: the Fact
// that caused another, each with the title of the Item it was drawn from.
type CauseConnection struct {
	Cause  Development
	Effect Development
	// Withdrawn is set once Feedback said it, or a Fact it connects, was
	// wrong.
	Withdrawn bool
	// Call is the kept connecting call that made it; zero if none was
	// kept.
	Call int64
}

// Development is a Fact that moves a Story on, with the title of the Item it
// was drawn from.
type Development struct {
	Fact
	ItemTitle string
	// Withdrawn is set once Feedback said its Fact was wrong, and Unwanted
	// once the Owner said it should not be in the Briefing. The acts never
	// see a withdrawn Development; the Record keeps it, marked. Missed is
	// set when the Owner added it, saying it was missed.
	Withdrawn, Unwanted, Missed bool
	// PlacingCall is the kept placing call that made it a Development;
	// zero if none was kept, or if the Owner added it.
	PlacingCall int64
}

// Placing is the result of placing an Item's Facts.
type Placing struct {
	SourceID string
	EntryID  string
	// Facts are the ids of every Fact placed, including those that became a
	// Development in no Story.
	Facts []int64
	// Developments are the Facts placed in existing Stories.
	Developments []Placement
	// NewStories are the Stories placing started.
	NewStories []NewStory
	// Precedents are the ids of the Feedback the act was shown as
	// precedent, kept with the result so the reason for it stays visible.
	Precedents []int64
	Model      string
	Usage      Usage
}

// Placement makes a Fact a Development in an existing Story.
type Placement struct {
	FactID  int64
	StoryID int64
}

// NewStory is a Story placing started, with the Facts that are its first
// Developments.
type NewStory struct {
	Title   string
	FactIDs []int64
}
