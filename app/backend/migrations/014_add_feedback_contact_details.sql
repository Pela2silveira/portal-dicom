-- Extra context so an operator can follow up: where the study was done, and
-- a phone only when the person chooses to leave one.
ALTER TABLE feedback_comments
  ADD COLUMN IF NOT EXISTS study_site text;

ALTER TABLE feedback_comments
  ADD COLUMN IF NOT EXISTS contact_phone text;
