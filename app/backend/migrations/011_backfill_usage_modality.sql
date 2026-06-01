-- Backfill the `modality` dim on historical study.download / study.retrieve
-- usage events that predate modality tracking. The modality is sourced from the
-- QIDO study cache (qido_study_cache.modalities_json) joined on the study UID
-- that every download/retrieve event already records in dims->>'study_uid'.
--
-- Notes / limitations:
--   * Studies absent from the cache (never queried via QIDO, or evicted) stay
--     UNKNOWN — they cannot be resolved retroactively.
--   * Idempotent: only rows whose modality is missing/empty/UNKNOWN are touched,
--     so it is safe to re-run by hand (e.g. `psql -f`) after the cache fills up.
--   * Modalities are upper-cased, de-duplicated and sorted, matching the live
--     normalization in usageModalityDim (so "CT/MR" buckets are consistent).
WITH study_modality AS (
  SELECT
    c.study_instance_uid,
    string_agg(DISTINCT upper(trim(m.value)), '/' ORDER BY upper(trim(m.value))) AS modality
  FROM qido_study_cache c
  CROSS JOIN LATERAL jsonb_array_elements_text(c.modalities_json) AS m(value)
  WHERE trim(m.value) <> ''
  GROUP BY c.study_instance_uid
)
UPDATE usage_events e
SET dims = COALESCE(e.dims, '{}'::jsonb) || jsonb_build_object('modality', sm.modality)
FROM study_modality sm
WHERE e.action IN ('study.download', 'study.retrieve')
  AND e.dims->>'study_uid' = sm.study_instance_uid
  AND COALESCE(NULLIF(e.dims->>'modality', ''), 'UNKNOWN') = 'UNKNOWN'
  AND sm.modality IS NOT NULL
  AND sm.modality <> '';
