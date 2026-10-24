-- A Briefing written for a day. Only its day for now: what the page needs to
-- know whether today's Briefing exists.
CREATE TABLE briefings (
    day TEXT PRIMARY KEY -- YYYY-MM-DD, in the Owner's time zone
) STRICT;
