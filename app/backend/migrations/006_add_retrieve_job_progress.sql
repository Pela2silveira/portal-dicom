ALTER TABLE retrieve_jobs
  ADD COLUMN IF NOT EXISTS orthanc_job_id text,
  ADD COLUMN IF NOT EXISTS phase text,
  ADD COLUMN IF NOT EXISTS progress integer NOT NULL DEFAULT 0;
