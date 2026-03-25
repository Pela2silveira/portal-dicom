ALTER TABLE physicians
  ADD COLUMN IF NOT EXISTS license_number text,
  ADD COLUMN IF NOT EXISTS licensed boolean NOT NULL DEFAULT false;
