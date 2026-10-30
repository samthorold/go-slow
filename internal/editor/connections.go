package editor

// ToConnect is an Item's Facts awaiting connecting. They are connected
// together, in one call, so a cause between two Facts of one Item is seen.
type ToConnect struct {
	Item  Item
	Facts []Fact
	// Mentions are the Entities the Item mentions: every Entity its reading
	// gave, in any of its Facts or Claims, each once. Connecting chooses from
	// them the Entities each Fact involves; a role given to any other name is
	// refused.
	Mentions []Mention
}

// Mention is an Entity an Item mentions, by its name and kind.
type Mention struct {
	Name string
	Kind string
}

// Connecting is the result of connecting an Item's Facts.
type Connecting struct {
	SourceID string
	EntryID  string
	// Facts are the ids of every Fact connected, including those given no
	// cause.
	Facts []int64
	// Roles are the roles each Fact's Entities have in it. A role may go to
	// an Entity of the Item's reading the Fact did not list, which the Fact
	// then involves.
	Roles []Role
	// Causes are the cause Connections made, each between two Facts.
	Causes []Cause
	Model  string
	Usage  Usage
}

// Role is the role an Entity has in a Fact: a Connection from the Fact to
// the Entity.
type Role struct {
	FactID int64
	Entity string
	Role   string
}

// Cause is a cause Connection: one Fact caused another.
type Cause struct {
	CauseID  int64
	EffectID int64
}
