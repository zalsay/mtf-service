-- Indexes for Open API watchlist-gated MTF reads.

CREATE INDEX IF NOT EXISTS idx_user_watchlist_user_symbol_canonical
ON user_watchlist (
    user_id,
    (CASE
        WHEN regexp_replace(lower(trim(symbol)), '[^0-9]', '', 'g') <> ''
        THEN regexp_replace(lower(trim(symbol)), '[^0-9]', '', 'g')
        ELSE lower(trim(symbol))
    END)
);

CREATE INDEX IF NOT EXISTS idx_mtf_best_predictions_unique_key
ON mtf_best_predictions(unique_key);
