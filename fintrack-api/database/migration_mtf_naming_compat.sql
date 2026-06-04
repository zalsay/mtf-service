BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_strategy_params' AND relkind = 'v') THEN
        DROP VIEW timesfm_strategy_params;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_best_predictions' AND relkind = 'v') THEN
        DROP VIEW timesfm_best_predictions;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_best_validation_chunks' AND relkind = 'v') THEN
        DROP VIEW timesfm_best_validation_chunks;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_backtests' AND relkind = 'v') THEN
        DROP VIEW timesfm_backtests;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_direct_predictions' AND relkind = 'v') THEN
        DROP VIEW timesfm_direct_predictions;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'timesfm_forecast' AND relkind = 'v') THEN
        DROP VIEW timesfm_forecast;
    END IF;

    IF to_regclass('timesfm_strategy_params') IS NOT NULL
       AND to_regclass('mtf_strategy_params') IS NULL THEN
        ALTER TABLE timesfm_strategy_params RENAME TO mtf_strategy_params;
    END IF;

    IF to_regclass('timesfm_best_predictions') IS NOT NULL
       AND to_regclass('mtf_best_predictions') IS NULL THEN
        ALTER TABLE timesfm_best_predictions RENAME TO mtf_best_predictions;
    END IF;

    IF to_regclass('timesfm_best_validation_chunks') IS NOT NULL
       AND to_regclass('mtf_best_validation_chunks') IS NULL THEN
        ALTER TABLE timesfm_best_validation_chunks RENAME TO mtf_best_validation_chunks;
    END IF;

    IF to_regclass('timesfm_backtests') IS NOT NULL
       AND to_regclass('mtf_backtests') IS NULL THEN
        ALTER TABLE timesfm_backtests RENAME TO mtf_backtests;
    END IF;

    IF to_regclass('timesfm_direct_predictions') IS NOT NULL
       AND to_regclass('mtf_direct_predictions') IS NULL THEN
        ALTER TABLE timesfm_direct_predictions RENAME TO mtf_direct_predictions;
    END IF;

    IF to_regclass('timesfm_forecast') IS NOT NULL
       AND to_regclass('mtf_forecast') IS NULL THEN
        ALTER TABLE timesfm_forecast RENAME TO mtf_forecast;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_best_predictions'
          AND column_name = 'timesfm_version'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_best_predictions'
          AND column_name = 'mtf_version'
    ) THEN
        ALTER TABLE mtf_best_predictions RENAME COLUMN timesfm_version TO mtf_version;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_backtests'
          AND column_name = 'timesfm_version'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_backtests'
          AND column_name = 'mtf_version'
    ) THEN
        ALTER TABLE mtf_backtests RENAME COLUMN timesfm_version TO mtf_version;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_direct_predictions'
          AND column_name = 'timesfm_version'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'mtf_direct_predictions'
          AND column_name = 'mtf_version'
    ) THEN
        ALTER TABLE mtf_direct_predictions RENAME COLUMN timesfm_version TO mtf_version;
    END IF;
END
$$;

UPDATE mtf_best_predictions
SET prediction_type = CASE
    WHEN lower(trim(prediction_type)) IN ('cov', 'pro', 'mtf_pro') THEN 'mtf-pro'
    WHEN lower(trim(prediction_type)) IN ('non_cov', 'non-cov', 'lite', 'mtf_lite', '') THEN 'mtf-lite'
    ELSE prediction_type
END
WHERE prediction_type IS NOT NULL;

UPDATE mtf_best_validation_chunks
SET prediction_type = CASE
    WHEN lower(trim(prediction_type)) IN ('cov', 'pro', 'mtf_pro') THEN 'mtf-pro'
    WHEN lower(trim(prediction_type)) IN ('non_cov', 'non-cov', 'lite', 'mtf_lite', '') THEN 'mtf-lite'
    ELSE prediction_type
END
WHERE prediction_type IS NOT NULL;

ALTER TABLE IF EXISTS mtf_best_predictions
    ALTER COLUMN prediction_type SET DEFAULT 'mtf-lite';

ALTER TABLE IF EXISTS mtf_best_validation_chunks
    ALTER COLUMN prediction_type SET DEFAULT 'mtf-lite';

ALTER TABLE IF EXISTS mtf_best_validation_chunks DROP CONSTRAINT IF EXISTS fk_timesfm_best;
ALTER TABLE IF EXISTS mtf_best_validation_chunks DROP CONSTRAINT IF EXISTS fk_mtf_best;

WITH mapped AS (
    SELECT unique_key AS old_key,
           CASE
               WHEN unique_key LIKE '%\_non\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_non_cov$', '_mtf-lite')
               WHEN unique_key LIKE '%\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_cov$', '_mtf-pro')
               ELSE unique_key
           END AS new_key
    FROM mtf_best_predictions
    WHERE unique_key LIKE '%\_non\_cov' ESCAPE '\'
       OR unique_key LIKE '%\_cov' ESCAPE '\'
)
UPDATE mtf_best_validation_chunks c
SET unique_key = mapped.new_key,
    updated_at = CURRENT_TIMESTAMP
FROM mapped
WHERE c.unique_key = mapped.old_key;

WITH mapped AS (
    SELECT unique_key AS old_key,
           CASE
               WHEN unique_key LIKE '%\_non\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_non_cov$', '_mtf-lite')
               WHEN unique_key LIKE '%\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_cov$', '_mtf-pro')
               ELSE unique_key
           END AS new_key
    FROM mtf_best_predictions
    WHERE unique_key LIKE '%\_non\_cov' ESCAPE '\'
       OR unique_key LIKE '%\_cov' ESCAPE '\'
)
UPDATE mtf_backtests b
SET unique_key = mapped.new_key,
    updated_at = CURRENT_TIMESTAMP
FROM mapped
WHERE b.unique_key = mapped.old_key;

UPDATE mtf_best_predictions
SET unique_key = CASE
        WHEN unique_key LIKE '%\_non\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_non_cov$', '_mtf-lite')
        WHEN unique_key LIKE '%\_cov' ESCAPE '\' THEN regexp_replace(unique_key, '_cov$', '_mtf-pro')
        ELSE unique_key
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE unique_key LIKE '%\_non\_cov' ESCAPE '\'
   OR unique_key LIKE '%\_cov' ESCAPE '\';

UPDATE mtf_direct_predictions
SET unique_key = replace(replace(unique_key, '_non_cov_', '_mtf-lite_'), '_cov_', '_mtf-pro_'),
    updated_at = CURRENT_TIMESTAMP
WHERE unique_key LIKE '%\_non\_cov\_%' ESCAPE '\'
   OR unique_key LIKE '%\_cov\_%' ESCAPE '\';

ALTER TABLE IF EXISTS mtf_best_validation_chunks
    ADD CONSTRAINT fk_mtf_best FOREIGN KEY (unique_key)
    REFERENCES mtf_best_predictions (unique_key) ON DELETE CASCADE;

CREATE OR REPLACE VIEW timesfm_strategy_params AS SELECT * FROM mtf_strategy_params;
CREATE OR REPLACE VIEW timesfm_best_predictions AS SELECT * FROM mtf_best_predictions;
CREATE OR REPLACE VIEW timesfm_best_validation_chunks AS SELECT * FROM mtf_best_validation_chunks;
CREATE OR REPLACE VIEW timesfm_backtests AS SELECT * FROM mtf_backtests;
CREATE OR REPLACE VIEW timesfm_direct_predictions AS SELECT * FROM mtf_direct_predictions;
CREATE OR REPLACE VIEW timesfm_forecast AS SELECT * FROM mtf_forecast;

COMMIT;
