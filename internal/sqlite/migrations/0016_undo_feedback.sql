-- When the Owner undid a piece of Feedback; none while it stands. An undone
-- piece is kept as given, never deleted, and the marks it made on the
-- Record are lifted, unless another piece still standing makes them too.
ALTER TABLE feedback ADD COLUMN undone_at TEXT;
