import { PredictionChartData, PublicPredictionItem, StockData, MTFChunk } from '../types';

const PRIMARY_MODEL_LABEL = 'mtf-1.5-lite';
const PRO_MODEL_LABEL = 'mtf-1.5-pro';

export interface ResolvedPublicPrediction {
    primary: PublicPredictionItem;
    pro?: PublicPredictionItem;
}

export type MTFPredictionType = 'mtf-lite' | 'mtf-pro';

export const normalizeMTFPredictionType = (value: unknown): MTFPredictionType | null => {
    const normalized = String(value || '').trim().toLowerCase().replace(/_/g, '-');
    if (normalized === 'mtf-pro' || normalized === 'mtf-lite') {
        return normalized;
    }
    return null;
};

export const isMTFProUniqueKey = (value: unknown): boolean => {
    const normalized = String(value || '').toLowerCase();
    return normalized.includes('_mtf-pro') || normalized.includes('_mtf_pro');
};

export const isMTFProPredictionItem = (item?: PublicPredictionItem | null): boolean => {
    if (!item) {
        return false;
    }
    const predictionType = normalizeMTFPredictionType(item.best.prediction_type);
    if (predictionType) {
        return predictionType === 'mtf-pro';
    }
    return isMTFProUniqueKey(item.best.unique_key);
};

const getSeriesChangePercent = (values: number[] | undefined): number | undefined => {
    if (!values?.length) {
        return undefined;
    }

    const start = Number(values[0]);
    const end = Number(values[values.length - 1]);
    if (!(start > 0) || !Number.isFinite(end)) {
        return undefined;
    }

    return ((end - start) / start) * 100;
};

const toFiniteNumberOrNaN = (value: unknown): number => {
    const num = Number(value);
    return Number.isFinite(num) ? num : Number.NaN;
};

const isRecord = (value: unknown): value is Record<string, unknown> => (
    typeof value === 'object' && value !== null && !Array.isArray(value)
);

const toFiniteNumberArray = (value: unknown): number[] => (
    Array.isArray(value)
        ? value.map(Number).filter(Number.isFinite)
        : []
);

const toPredictionMap = (value: unknown): Record<string, number[]> | undefined => {
    if (!isRecord(value)) {
        return undefined;
    }
    const out: Record<string, number[]> = {};
    Object.entries(value).forEach(([key, series]) => {
        const values = toFiniteNumberArray(series);
        if (values.length > 0) {
            out[key] = values;
        }
    });
    return Object.keys(out).length > 0 ? out : undefined;
};

const normalizeAdjustRawChunk = (chunk: MTFChunk): Partial<MTFChunk> | null => {
    if (Number(chunk.stock_type || 0) !== 1 || !chunk.adjust_raw_chunks) {
        return null;
    }
    const raw = Array.isArray(chunk.adjust_raw_chunks)
        ? chunk.adjust_raw_chunks.find(isRecord)
        : chunk.adjust_raw_chunks;
    if (!isRecord(raw)) {
        return null;
    }
    return {
        predictions: toPredictionMap(raw.predictions),
        actual_values: toFiniteNumberArray(raw.actual_values),
        predicted_change_percent: toPredictionMap(raw.predicted_change_percent),
        actual_change_percent: toFiniteNumberArray(raw.actual_change_percent),
        change_base_value: Number.isFinite(Number(raw.change_base_value)) ? Number(raw.change_base_value) : chunk.change_base_value,
        change_base_date: typeof raw.change_base_date === 'string' ? raw.change_base_date : chunk.change_base_date,
        dates: Array.isArray(raw.dates) ? raw.dates.map(String).filter(Boolean) : undefined,
    };
};

const currentPriceChunkView = (chunk: MTFChunk): MTFChunk => {
    const raw = normalizeAdjustRawChunk(chunk);
    if (!raw) {
        return chunk;
    }
    return {
        ...chunk,
        ...raw,
        predictions: raw.predictions || chunk.predictions,
        actual_values: raw.actual_values?.length ? raw.actual_values : chunk.actual_values,
        predicted_change_percent: raw.predicted_change_percent || chunk.predicted_change_percent,
        actual_change_percent: raw.actual_change_percent?.length ? raw.actual_change_percent : chunk.actual_change_percent,
        dates: raw.dates?.length ? raw.dates : chunk.dates,
    };
};

const compareUpdatedAt = (left?: string, right?: string): number => {
    const leftTime = left ? new Date(left).getTime() : 0;
    const rightTime = right ? new Date(right).getTime() : 0;
    return leftTime - rightTime;
};

const selectLatestItem = (
    current: PublicPredictionItem | undefined,
    next: PublicPredictionItem,
): PublicPredictionItem => {
    if (!current) {
        return next;
    }
    return compareUpdatedAt(current.best.updated_at, next.best.updated_at) <= 0 ? next : current;
};

const isMTFProPrediction = (item: PublicPredictionItem): boolean => {
    return isMTFProPredictionItem(item);
};

const buildPredictionGroupKey = (item: PublicPredictionItem): string => {
    return [
        item.best.symbol,
        item.best.horizon_len ?? '',
        item.best.mtf_version ?? '',
    ].join('|');
};

const parseMetrics = (metrics: unknown): Record<string, unknown> => {
    if (!metrics) {
        return {};
    }
    if (typeof metrics === 'string') {
        try {
            const parsed = JSON.parse(metrics);
            return parsed && typeof parsed === 'object' ? parsed : {};
        } catch {
            return {};
        }
    }
    return typeof metrics === 'object' ? metrics as Record<string, unknown> : {};
};

const ETF_CODE_PREFIXES = [
    '159',
    '16',
    '50',
    '51',
    '52',
    '56',
    '58',
] as const;

const inferStockType = (symbol: string, companyName?: string): 1 | 2 => {
    const normalizedCompanyName = String(companyName || '').toUpperCase();
    if (normalizedCompanyName.includes('ETF')) {
        return 2;
    }

    const digits = String(symbol || '').replace(/\D/g, '').slice(0, 6);
    if (!digits) {
        return 1;
    }

    return ETF_CODE_PREFIXES.some(prefix => digits.startsWith(prefix)) ? 2 : 1;
};

const extractSeriesFromChunks = (
    chunks: MTFChunk[] | undefined,
    bestItemKey: string,
): PredictionChartData | null => {
    if (!bestItemKey || !chunks?.length) {
        return null;
    }

    const sortedChunks = [...chunks].sort(
        (a, b) => new Date(a.start_date).getTime() - new Date(b.start_date).getTime(),
    );

    const dates: string[] = [];
    const actuals: number[] = [];
    const predictions: number[] = [];
    const actualChangePercents: number[] = [];
    const predictedChangePercents: number[] = [];

    for (const originalChunk of sortedChunks) {
        const chunk = currentPriceChunkView(originalChunk);
        const chunkPredictions = Array.isArray(chunk.predictions?.[bestItemKey])
            ? chunk.predictions[bestItemKey]
            : [];
        const chunkActualChangePercents = Array.isArray(chunk.actual_change_percent)
            ? chunk.actual_change_percent
            : [];
        const chunkPredictedChangePercents = Array.isArray(chunk.predicted_change_percent?.[bestItemKey])
            ? chunk.predicted_change_percent[bestItemKey]
            : [];
        const maxLen = Math.min(
            chunk.dates?.length || 0,
            chunk.actual_values?.length || 0,
            chunkPredictions.length,
        );

        for (let index = 0; index < maxLen; index += 1) {
            const date = String(chunk.dates[index] || '').trim();
            const actual = Number(chunk.actual_values[index]);
            const predicted = Number(chunkPredictions[index]);

            if (!date) {
                continue;
            }

            dates.push(date);
            actuals.push(Number.isFinite(actual) ? actual : 0);
            predictions.push(Number.isFinite(predicted) ? predicted : 0);
            actualChangePercents.push(toFiniteNumberOrNaN(chunkActualChangePercents[index]));
            predictedChangePercents.push(toFiniteNumberOrNaN(chunkPredictedChangePercents[index]));
        }
    }

    if (dates.length === 0) {
        return null;
    }

    return {
        dates,
        actuals,
        predictions,
        actualChangePercents,
        predictedChangePercents,
    };
};

const alignSeriesByDate = (
    baseDates: string[],
    proSeries: PredictionChartData | null,
    getValue: (series: PredictionChartData) => number[] | undefined,
    missingValue = 0,
): number[] | undefined => {
    if (!proSeries || proSeries.dates.length === 0) {
        return undefined;
    }

    const values = getValue(proSeries) || [];
    const predictionByDate = new Map<string, number>();
    proSeries.dates.forEach((date, index) => {
        const predicted = Number(values[index]);
        if (date && Number.isFinite(predicted)) {
            predictionByDate.set(date, predicted);
        }
    });

    const aligned = baseDates.map(date => predictionByDate.get(date) ?? missingValue);
    return aligned.some(value => Number.isFinite(value) && value !== 0) ? aligned : undefined;
};

const buildPredictionAnalysis = (
    displayModelName: string,
    contextLen: number | undefined,
    horizonLen: number | undefined,
    hasProLine: boolean,
    language: string,
): string => {
    const formattedContext = contextLen
        ? (contextLen < 1024 ? contextLen : `${Math.round(contextLen / 1024)}K`)
        : '?';
    const suffix = hasProLine ? (language === 'zh' ? ` · 含 ${PRO_MODEL_LABEL}` : ` · with ${PRO_MODEL_LABEL}`) : '';

    if (language === 'zh') {
        return `最佳模型: ${displayModelName} 上下文: ${formattedContext} 预测: ${horizonLen || '?'}天${suffix}`;
    }
    return `Best: ${displayModelName} Ctx: ${formattedContext} Hor: ${horizonLen || '?'}d${suffix}`;
};

export const resolvePublicPredictionItems = (items: PublicPredictionItem[] = []): ResolvedPublicPrediction[] => {
    const grouped = new Map<string, { primary?: PublicPredictionItem; pro?: PublicPredictionItem }>();

    for (const item of items) {
        if (!item?.best?.symbol || !item.best.best_prediction_item) {
            continue;
        }

        const key = buildPredictionGroupKey(item);
        const bucket = grouped.get(key) || {};

        if (isMTFProPrediction(item)) {
            bucket.pro = selectLatestItem(bucket.pro, item);
        } else {
            bucket.primary = selectLatestItem(bucket.primary, item);
        }

        grouped.set(key, bucket);
    }

    return Array.from(grouped.values())
        .map((bucket): ResolvedPublicPrediction | null => {
            const primary = bucket.primary || bucket.pro;
            if (!primary) {
                return null;
            }

            return {
                primary,
                pro: bucket.primary ? bucket.pro : undefined,
            };
        })
        .filter((item): item is ResolvedPublicPrediction => item !== null);
};

export const mapResolvedPredictionToStockData = (
    resolved: ResolvedPublicPrediction,
    language: string,
): StockData | null => {
    const primary = resolved.primary;
    const bestItemKey = primary.best.best_prediction_item;
    const contextLen = primary.best.context_len;
    const horizonLen = primary.best.horizon_len;
    const series = extractSeriesFromChunks(primary.chunks, bestItemKey);
    const isProOnly = isMTFProPrediction(primary) && !resolved.pro;

    if (!series) {
        return null;
    }

    const liteSeries = isProOnly ? null : series;
    const proSource = resolved.pro || (isProOnly ? primary : undefined);
    const proSeries = proSource
        ? extractSeriesFromChunks(proSource.chunks, proSource.best.best_prediction_item)
        : null;
    const proPredictions = proSeries ? alignSeriesByDate(series.dates, proSeries, value => value.predictions) : undefined;
    const proPredictedChangePercents = proSeries
        ? alignSeriesByDate(series.dates, proSeries, value => value.predictedChangePercents, Number.NaN)
        : undefined;
    const proMetrics = proSource ? parseMetrics(proSource.best.best_metrics) : {};

    const lastActual = series.actuals.length > 0 ? series.actuals[series.actuals.length - 1] : 0;
    const lastPred = liteSeries && liteSeries.predictions.length > 0
        ? liteSeries.predictions[liteSeries.predictions.length - 1]
        : 0;
    const proLastPred = proSeries && proSeries.predictions.length > 0
        ? proSeries.predictions[proSeries.predictions.length - 1]
        : undefined;
    const displayPredictionPrice = isProOnly ? (proLastPred ?? lastPred) : lastPred;
    const price = lastActual || displayPredictionPrice;

    const startActual = series.actuals.length > 0 ? series.actuals[0] : 0;
    const changePercent = startActual > 0 ? ((lastActual - startActual) / startActual) * 100 : 0;

    const predictedChangePercent = liteSeries ? getSeriesChangePercent(liteSeries.predictions) : undefined;

    const metrics = parseMetrics(primary.best.best_metrics);
    const compositeScore = Number(metrics.composite_score);
    const confidence = Number.isFinite(compositeScore) ? 100 - compositeScore : 85;
    const proCompositeScore = Number(proMetrics.composite_score);
    const proConfidence = Number.isFinite(proCompositeScore) ? 100 - proCompositeScore : undefined;
    const proPredictedChangePercent = proSeries ? getSeriesChangePercent(proSeries.predictions) : undefined;
    const effectivePredictedChangePercent = predictedChangePercent ?? proPredictedChangePercent ?? 0;
    const displayModelName = isProOnly ? PRO_MODEL_LABEL : PRIMARY_MODEL_LABEL;
    const displayConfidence = isProOnly
        ? (proConfidence !== undefined ? proConfidence : confidence)
        : confidence;
    const watchlistCount = Math.max(
        Number(primary.best.watchlist_count ?? 0),
        Number(resolved.pro?.best.watchlist_count ?? 0),
    );

    return {
        symbol: primary.best.symbol,
        companyName: primary.best.short_name || primary.best.symbol,
        stockType: inferStockType(primary.best.symbol, primary.best.short_name),
        watchlistCount: Number.isFinite(watchlistCount) ? watchlistCount : 0,
        currentPrice: price,
        changePercent,
        predictedChangePercent,
        prediction: {
            predicted_high: displayPredictionPrice,
            predicted_low: displayPredictionPrice,
            confidence: parseFloat(displayConfidence.toFixed(4)),
            sentiment: effectivePredictedChangePercent >= 0 ? 'Bullish' : 'Bearish',
            analysis: buildPredictionAnalysis(
                displayModelName,
                contextLen,
                horizonLen,
                !isProOnly && Boolean(proPredictions),
                language,
            ),
            modelName: isProOnly ? undefined : PRIMARY_MODEL_LABEL,
            contextLen,
            horizonLen,
            maxDeviationPercent: primary.max_deviation_percent,
            proModelName: proSource ? PRO_MODEL_LABEL : undefined,
            proContextLen: proSource?.best.context_len,
            proHorizonLen: proSource?.best.horizon_len,
            proConfidence: proConfidence !== undefined ? parseFloat(proConfidence.toFixed(4)) : undefined,
            proMaxDeviationPercent: proSource?.max_deviation_percent,
            proPredictedChangePercent,
            chartData: {
                ...series,
                predictions: liteSeries?.predictions || [],
                predictedChangePercents: liteSeries?.predictedChangePercents || [],
                proPredictions,
                proPredictedChangePercents,
            },
        },
    };
};

export const mapPredictionResponseToStockData = (res: any, language: string): StockData[] => {
    if (!res?.items) {
        return [];
    }

    return resolvePublicPredictionItems(res.items)
        .map(item => mapResolvedPredictionToStockData(item, language))
        .filter((item): item is StockData => item !== null);
};
