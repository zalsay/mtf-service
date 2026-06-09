import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import PredictionChartPanel from '../common/PredictionChartPanel';
import {
    DirectPredictionResult,
    getAccessiblePredictions,
    MTFJobStatusResponse,
    MTFPredictAcceptedResponse,
    MTFPredictOnceRequest,
    WatchlistItem,
    mtfAPI,
} from '../../services/apiService';
import type { PredictionChartData, PublicPredictionItem } from '../../types';
import { flattenPublicPredictionItems, isMTFProPredictionItem, resolvePublicPredictionItems } from '../../utils/predictionUtils';

interface SinglePredictionModalProps {
    isOpen: boolean;
    item: WatchlistItem | null;
    currentPrice?: number;
    mode?: 'standalone' | 'next_chunk';
    predictionType?: 'mtf-lite' | 'mtf-pro';
    initialPredictionType?: 'mtf-lite' | 'mtf-pro';
    predictionTypeOptions?: Array<'mtf-lite' | 'mtf-pro'>;
    covariateSignature?: string;
    initialHorizonLen?: number;
    initialContextLen?: number;
    horizonOptions?: number[];
    contextOptions?: number[];
    mtfVersion?: string;
    enableCachedLookup?: boolean;
    historicalBestItem?: PublicPredictionItem | null;
    onClose: () => void;
    onAuthError?: () => void;
    onSubmittingChange?: (isSubmitting: boolean) => void;
    onPredictionComplete?: (result: DirectPredictionResult, request: MTFPredictOnceRequest) => void | Promise<void>;
}

const DEFAULT_HORIZON_LEN = 7;
const DEFAULT_CONTEXT_LEN = 2048;
const DEFAULT_TIME_STEP = 0;
const DEFAULT_YEARS = 15;
const DEFAULT_MTF_VERSION = '2.5';
const HORIZON_OPTIONS = [7, 14, 28];
const CONTEXT_OPTIONS = [512, 1024, 2048];
const POLL_INTERVAL_MS = 2000;
const MAX_POLL_ATTEMPTS = 180;

const sleep = (ms: number) => new Promise(resolve => window.setTimeout(resolve, ms));

const isRecord = (value: unknown): value is Record<string, unknown> => (
    Boolean(value) && typeof value === 'object' && !Array.isArray(value)
);

const isDirectPredictionResult = (value: unknown): value is DirectPredictionResult => (
    isRecord(value) && Array.isArray(value.future_dates)
);

const findDirectPredictionResult = (value: unknown, depth = 0): DirectPredictionResult | null => {
    if (depth > 4 || !isRecord(value)) {
        return null;
    }

    if (isDirectPredictionResult(value)) {
        return value;
    }

    for (const key of ['data', 'result', 'payload', 'response']) {
        const nested = findDirectPredictionResult(value[key], depth + 1);
        if (nested) {
            return nested;
        }
    }

    return null;
};

const extractDirectPredictionResult = (job: MTFJobStatusResponse): DirectPredictionResult | null => {
    return findDirectPredictionResult(job.result);
};

const normalizeJobError = (job: MTFJobStatusResponse): string => {
    const result = isRecord(job.result) ? job.result : {};
    const resultError = typeof result.error === 'string' ? result.error : '';
    const resultMessage = typeof result.message === 'string' ? result.message : '';
    return job.error || resultError || resultMessage || 'Prediction job failed';
};

const getJobProgressLabel = (
    snapshot: MTFJobStatusResponse | MTFPredictAcceptedResponse | null,
    language: string,
): string => {
    const status = String(snapshot?.status || '').toLowerCase();
    const stage = String(snapshot?.current_stage || '').toLowerCase();
    const backend = String((snapshot as MTFJobStatusResponse | null)?.backend || '').toLowerCase();
    const isZh = language === 'zh';

    if (status === 'succeeded') {
        return isZh ? '已完成' : 'Completed';
    }
    if (status === 'failed') {
        return isZh ? '失败' : 'Failed';
    }
    if (status === 'running') {
        if (stage === 'xreg' || backend.includes('xreg')) {
            return isZh ? 'Pro 模型处理中' : 'Pro model processing';
        }
        if (stage === 'main') {
            return isZh ? '主模型预测中' : 'Main model forecasting';
        }
        return isZh ? '预测执行中' : 'Prediction running';
    }
    if (status === 'queued' || status === 'pending') {
        return isZh ? '排队中' : 'Queued';
    }

    return isZh ? '准备提交' : 'Preparing';
};

const normalizePredictionSymbol = (value: string): string => {
    const trimmed = String(value || '').trim().toLowerCase();
    const digits = trimmed.replace(/\D/g, '');
    return digits || trimmed;
};

const normalizeOptionalText = (value: unknown): string => String(value || '').trim().toLowerCase();

const isMTFProPredictionItemLocal = (item?: PublicPredictionItem | null): boolean => {
    return isMTFProPredictionItem(item);
};

const doesHistoricalBestMatchSelection = (
    candidate: PublicPredictionItem | null | undefined,
    symbol: string,
    predictionType: 'mtf-lite' | 'mtf-pro',
    contextLen: number,
    horizonLen: number,
    mtfVersion: string,
): boolean => {
    if (!candidate) {
        return false;
    }
    if (normalizePredictionSymbol(candidate.best.symbol) !== normalizePredictionSymbol(symbol)) {
        return false;
    }
    if (Number(candidate.best.context_len || 0) !== contextLen) {
        return false;
    }
    if (Number(candidate.best.horizon_len || 0) !== horizonLen) {
        return false;
    }
    if (String(candidate.best.mtf_version || '').trim() !== mtfVersion) {
        return false;
    }
    return predictionType === 'mtf-pro'
        ? isMTFProPredictionItemLocal(candidate)
        : !isMTFProPredictionItemLocal(candidate);
};

const parseChunkDateTime = (value: unknown): number => {
    const raw = String(value || '').trim();
    if (!raw) {
        return 0;
    }
    if (/^\d{8}$/.test(raw)) {
        const normalized = `${raw.slice(0, 4)}-${raw.slice(4, 6)}-${raw.slice(6, 8)}`;
        return new Date(normalized).getTime() || 0;
    }
    return new Date(raw).getTime() || 0;
};

const pickLatestHistoricalChunk = (
    chunks: PublicPredictionItem['chunks'],
    bestKey: string,
    latestDataDate?: string | null,
) => {
    const latestDataTime = parseChunkDateTime(latestDataDate);
    const sortedChunks = [...(chunks || [])].sort(
        (a, b) => parseChunkDateTime(a.start_date) - parseChunkDateTime(b.start_date),
    );

    const hasDrawableHistory = (sourceChunk: PublicPredictionItem['chunks'][number]) => {
        const chunk = currentPriceChunkView(sourceChunk);
        const chunkPredictions = getPredictionSeries(chunk.predictions, bestKey);
        return (
            (chunk.dates?.length || 0) > 0
            && (chunk.actual_values?.length || 0) > 0
            && chunkPredictions.length > 0
        );
    };

    const historicalChunks = sortedChunks.filter(chunk => {
        if (!hasDrawableHistory(chunk)) {
            return false;
        }
        if (!latestDataTime) {
            return true;
        }
        return parseChunkDateTime(chunk.start_date) <= latestDataTime;
    });

    return historicalChunks[historicalChunks.length - 1] || null;
};

const appendLatestActualAnchor = (
    result: DirectPredictionResult,
    dates: string[],
    actuals: number[],
    predictions: number[],
    actualChangePercents: number[],
    predictedChangePercents: number[],
) => {
    const latestDate = String(result.latest_data_date || result.request_end_date || '').trim();
    const latestClose = getDirectLatestClose(result);
    if (!latestDate || !(latestClose > 0)) {
        return;
    }

    const lastDate = dates[dates.length - 1];
    if (lastDate && parseChunkDateTime(latestDate) <= parseChunkDateTime(lastDate)) {
        return;
    }

    dates.push(latestDate);
    actuals.push(latestClose);
    predictions.push(0);
    actualChangePercents.push(Number.NaN);
    predictedChangePercents.push(Number.NaN);
};

const pickHistoricalBestItem = (
    items: PublicPredictionItem[],
    symbol: string,
    predictionType: 'mtf-lite' | 'mtf-pro',
    contextLen: number,
    horizonLen: number,
    mtfVersion: string,
): PublicPredictionItem | null => {
    const normalizedSymbol = normalizePredictionSymbol(symbol);
    const resolvedItems = resolvePublicPredictionItems(items || []);

    for (const resolved of resolvedItems) {
        const candidate = predictionType === 'mtf-pro'
            ? (resolved.pro || (isMTFProPredictionItemLocal(resolved.primary) ? resolved.primary : undefined))
            : (isMTFProPredictionItemLocal(resolved.primary) ? undefined : resolved.primary);
        if (!candidate) {
            continue;
        }
        if (!doesHistoricalBestMatchSelection(candidate, normalizedSymbol, predictionType, contextLen, horizonLen, mtfVersion)) {
            continue;
        }
        return candidate;
    }

    return null;
};

const getDirectPredictionValues = (
    result: DirectPredictionResult,
    preferredKey: string,
): number[] => {
    const rawValues = Array.isArray(result.adjust_raw_best_prediction_values)
        ? result.adjust_raw_best_prediction_values
        : [];
    if (Number(result.stock_type || 0) === 1 && rawValues.length > 0) {
        return rawValues.map(Number).filter(Number.isFinite);
    }

    const rawPreferredValues = Number(result.stock_type || 0) === 1
        && preferredKey
        && Array.isArray(result.adjust_raw_predictions?.[preferredKey])
        ? result.adjust_raw_predictions[preferredKey]
        : [];
    if (rawPreferredValues.length > 0) {
        return rawPreferredValues.map(Number).filter(Number.isFinite);
    }

    const directValues = Array.isArray(result.best_prediction_values)
        ? result.best_prediction_values
        : [];
    if (directValues.length > 0) {
        return directValues.map(Number).filter(Number.isFinite);
    }

    const preferredValues = preferredKey && Array.isArray(result.predictions?.[preferredKey])
        ? result.predictions[preferredKey]
        : [];
    if (preferredValues.length > 0) {
        return preferredValues.map(Number).filter(Number.isFinite);
    }

    const firstValues = Object.values(result.predictions || {}).find(Array.isArray) || [];
    return firstValues.map(Number).filter(Number.isFinite);
};

const getDirectLatestClose = (result: DirectPredictionResult): number => {
    const rawLatestClose = Number(result.adjust_raw_latest_close);
    if (Number(result.stock_type || 0) === 1 && rawLatestClose > 0) {
        return rawLatestClose;
    }
    return Number(result.latest_close);
};

const getPredictionSeries = (
    predictions: number[] | Record<string, number[]> | undefined,
    preferredKey: string,
): number[] => {
    if (Array.isArray(predictions)) {
        return predictions;
    }
    if (preferredKey && Array.isArray(predictions?.[preferredKey])) {
        return predictions[preferredKey];
    }
    return Object.values(predictions || {}).find(Array.isArray) || [];
};

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

const currentPriceChunkView = (chunk: PublicPredictionItem['chunks'][number]) => {
    if (Number(chunk.stock_type || 0) !== 1 || !chunk.adjust_raw_chunks) {
        return chunk;
    }
    const raw = Array.isArray(chunk.adjust_raw_chunks)
        ? chunk.adjust_raw_chunks.find(isRecord)
        : chunk.adjust_raw_chunks;
    if (!isRecord(raw)) {
        return chunk;
    }
    const rawActuals = toFiniteNumberArray(raw.actual_values);
    const rawActualChange = toFiniteNumberArray(raw.actual_change_percent);
    const rawDates = Array.isArray(raw.dates) ? raw.dates.map(String).filter(Boolean) : [];
    return {
        ...chunk,
        predictions: toPredictionMap(raw.predictions) || chunk.predictions,
        actual_values: rawActuals.length > 0 ? rawActuals : chunk.actual_values,
        predicted_change_percent: toPredictionMap(raw.predicted_change_percent) || chunk.predicted_change_percent,
        actual_change_percent: rawActualChange.length > 0 ? rawActualChange : chunk.actual_change_percent,
        change_base_value: Number.isFinite(Number(raw.change_base_value)) ? Number(raw.change_base_value) : chunk.change_base_value,
        change_base_date: typeof raw.change_base_date === 'string' ? raw.change_base_date : chunk.change_base_date,
        dates: rawDates.length > 0 ? rawDates : chunk.dates,
    };
};

const buildPredictionChartData = (
    result: DirectPredictionResult,
    predictionType: 'mtf-lite' | 'mtf-pro',
    latestClose?: number,
    fallbackCurrentPrice?: number,
    historicalBestItem?: PublicPredictionItem | null,
): PredictionChartData => {
    const bestKey = historicalBestItem?.best.best_prediction_item || result.best_prediction_item || '';
    const lastChunk = pickLatestHistoricalChunk(
        historicalBestItem?.chunks || [],
        bestKey,
        result.latest_data_date || result.request_end_date,
    );
    const futureDates = (result.future_dates || []).map(String).filter(Boolean);
    const futurePredictions = getDirectPredictionValues(result, bestKey);
    const directPredictedChangePercents = getPredictionSeries(result.predicted_change_percent, bestKey);

    const dates: string[] = [];
    const actuals: number[] = [];
    const predictions: number[] = [];
    const actualChangePercents: number[] = [];
    const predictedChangePercents: number[] = [];

    if (lastChunk) {
        const currentPriceChunk = currentPriceChunkView(lastChunk);
        const chunkDates = Array.isArray(currentPriceChunk.dates) ? currentPriceChunk.dates : [];
        const chunkPredictions = getPredictionSeries(currentPriceChunk.predictions, bestKey);
        const historyLen = Math.min(
            chunkDates.length,
            Math.max(currentPriceChunk.actual_values?.length || 0, chunkPredictions.length),
        );

        dates.push(...chunkDates.slice(0, historyLen).map(String));
        for (let index = 0; index < historyLen; index += 1) {
            const actual = Number(currentPriceChunk.actual_values?.[index]);
            actuals.push(Number.isFinite(actual) ? actual : 0);
        }
        for (let index = 0; index < historyLen; index += 1) {
            const num = Number(chunkPredictions[index]);
            predictions.push(Number.isFinite(num) ? num : 0);
        }

        if (Array.isArray(currentPriceChunk.actual_change_percent)) {
            actualChangePercents.push(...currentPriceChunk.actual_change_percent.slice(0, historyLen).map(Number));
        }
        const chunkPredictedChangePercents = getPredictionSeries(currentPriceChunk.predicted_change_percent, bestKey);
        for (let index = 0; index < historyLen; index += 1) {
            const num = Number(chunkPredictedChangePercents[index]);
            predictedChangePercents.push(Number.isFinite(num) ? num : Number.NaN);
        }
    } else {
        const startDate = result.latest_data_date || result.request_end_date || '';
        const startValue = Number(latestClose || fallbackCurrentPrice || 0);
        if (startDate && startValue > 0) {
            dates.push(startDate);
            actuals.push(startValue);
            predictions.push(0);
        }
    }

    appendLatestActualAnchor(result, dates, actuals, predictions, actualChangePercents, predictedChangePercents);

    const changeBase = Number(result.change_base_value)
        || actuals[actuals.length - 1]
        || Number(lastChunk ? currentPriceChunkView(lastChunk).change_base_value : undefined)
        || getDirectLatestClose(result)
        || Number(fallbackCurrentPrice)
        || 0;
    const futureLen = Math.min(futureDates.length, futurePredictions.length);
    for (let index = 0; index < futureLen; index += 1) {
        const predicted = Number(futurePredictions[index]);
        dates.push(futureDates[index]);
        predictions.push(Number.isFinite(predicted) ? predicted : 0);
        const backendPredictedChange = Number(directPredictedChangePercents[index]);
        predictedChangePercents.push(
            Number.isFinite(backendPredictedChange)
                ? backendPredictedChange
                :
            changeBase > 0 && Number.isFinite(predicted)
                ? ((predicted - changeBase) / changeBase) * 100
                : Number.NaN,
        );
    }

    if (predictionType === 'mtf-pro') {
        return {
            dates,
            actuals,
            predictions: [],
            proPredictions: predictions,
            actualChangePercents,
            predictedChangePercents: [],
            proPredictedChangePercents: predictedChangePercents,
        };
    }

    return {
        dates,
        actuals,
        predictions,
        actualChangePercents,
        predictedChangePercents,
    };
};

const SinglePredictionModal: React.FC<SinglePredictionModalProps> = ({
    isOpen,
    item,
    currentPrice,
    mode = 'standalone',
    predictionType,
    initialPredictionType,
    predictionTypeOptions,
    covariateSignature,
    initialHorizonLen,
    initialContextLen,
    horizonOptions,
    contextOptions,
    mtfVersion,
    enableCachedLookup,
    historicalBestItem,
    onClose,
    onAuthError,
    onSubmittingChange,
    onPredictionComplete,
}) => {
    const { t, language } = useLanguage();
    const [horizonLen, setHorizonLen] = useState(DEFAULT_HORIZON_LEN);
    const [contextLen, setContextLen] = useState(DEFAULT_CONTEXT_LEN);
    const [selectedPredictionType, setSelectedPredictionType] = useState<'mtf-lite' | 'mtf-pro'>(predictionType || initialPredictionType || 'mtf-lite');
    const [isCheckingExisting, setIsCheckingExisting] = useState(enableCachedLookup ?? mode === 'standalone');
    const [checkedLookupKey, setCheckedLookupKey] = useState<string | null>(null);
    const [checkedHistoricalLookupKey, setCheckedHistoricalLookupKey] = useState<string | null>(null);
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [acceptedJob, setAcceptedJob] = useState<MTFPredictAcceptedResponse | null>(null);
    const [jobStatus, setJobStatus] = useState<MTFJobStatusResponse | null>(null);
    const [predictionResult, setPredictionResult] = useState<DirectPredictionResult | null>(null);
    const [fetchedHistoricalBestItem, setFetchedHistoricalBestItem] = useState<PublicPredictionItem | null>(null);
    const requestSeqRef = useRef(0);
    const historyRequestSeqRef = useRef(0);

    const symbol = (item?.stock?.symbol || '').toLowerCase();
    const companyName = item?.stock?.company_name || symbol;
    const stockTypeValue = ((item?.stock_type || 1) === 2) ? 'etf' : 'stock';
    const requestStockCode = stockTypeValue === 'stock' ? symbol.replace(/^(sh|sz)/, '') : symbol;
    const defaultHorizonLen = initialHorizonLen ?? DEFAULT_HORIZON_LEN;
    const defaultContextLen = initialContextLen ?? DEFAULT_CONTEXT_LEN;
    const effectiveHorizonOptions = horizonOptions?.length ? horizonOptions : HORIZON_OPTIONS;
    const effectiveContextOptions = contextOptions?.length ? contextOptions : CONTEXT_OPTIONS;
    const effectivePredictionTypeOptions = predictionTypeOptions?.length ? predictionTypeOptions : [];
    const effectivePredictionType = predictionType || selectedPredictionType;
    const shouldLookupCached = enableCachedLookup ?? mode === 'standalone';
    const effectiveMTFVersion = mtfVersion || DEFAULT_MTF_VERSION;
    const matchingHistoricalBestItem = doesHistoricalBestMatchSelection(
        historicalBestItem,
        symbol,
        effectivePredictionType,
        contextLen,
        horizonLen,
        effectiveMTFVersion,
    ) ? historicalBestItem : null;
    const effectiveHistoricalBestItem = matchingHistoricalBestItem || fetchedHistoricalBestItem;
    const effectiveCovariateSignature = effectivePredictionType === 'mtf-pro'
        ? String(covariateSignature || effectiveHistoricalBestItem?.best.covariate_signature || '').trim()
        : '';
    const bestSeriesKey = predictionResult?.best_prediction_item || '';
    const selectedSeries = predictionResult?.best_prediction_values
        || (bestSeriesKey ? predictionResult?.predictions?.[bestSeriesKey] : undefined)
        || [];
    const numericSelectedSeries = selectedSeries.map(Number).filter(Number.isFinite);
    const latestClose = predictionResult?.latest_close ?? currentPrice;
    const latestPrediction = selectedSeries.length > 0 ? selectedSeries[selectedSeries.length - 1] : undefined;
    const backendPredictedChangeSeries = predictionResult && bestSeriesKey
        ? getPredictionSeries(predictionResult.predicted_change_percent, bestSeriesKey).map(Number).filter(Number.isFinite)
        : [];
    const predictedChangePercent = backendPredictedChangeSeries.length > 0
        ? backendPredictedChangeSeries[backendPredictedChangeSeries.length - 1]
        : latestClose && latestPrediction
        ? ((latestPrediction - latestClose) / latestClose) * 100
        : 0;
    const directChartData: PredictionChartData | null = predictionResult
        ? buildPredictionChartData(predictionResult, effectivePredictionType, latestClose, currentPrice, effectiveHistoricalBestItem)
        : null;
    const requestParams: MTFPredictOnceRequest = {
        stock_code: requestStockCode,
        stock_type: stockTypeValue,
        time_step: DEFAULT_TIME_STEP,
        years: DEFAULT_YEARS,
        horizon_len: horizonLen,
        context_len: contextLen,
        prediction_type: effectivePredictionType,
        ...(effectivePredictionType === 'mtf-pro'
            ? {
                covariate_preset: 'market_cov_v1',
                ...(effectiveCovariateSignature ? { covariate_signature: effectiveCovariateSignature } : {}),
            }
            : {}),
    };
    const cacheLookupKey = shouldLookupCached && item && !(effectivePredictionType === 'mtf-pro' && !effectiveCovariateSignature)
        ? [
            requestStockCode,
            stockTypeValue,
            horizonLen,
            contextLen,
            effectiveMTFVersion,
            effectivePredictionType,
            effectiveCovariateSignature,
        ].join('|')
        : '';
    const historicalLookupKey = isOpen && item && !matchingHistoricalBestItem
        ? [
            symbol,
            effectivePredictionType,
            contextLen,
            horizonLen,
            effectiveMTFVersion,
        ].join('|')
        : '';
    const hasCompletedHistoricalLookup = Boolean(historicalLookupKey)
        && checkedHistoricalLookupKey === historicalLookupKey;
    const hasUsableHistoricalBest = Boolean(effectiveHistoricalBestItem)
        && (effectivePredictionType !== 'mtf-pro' || Boolean(effectiveCovariateSignature));
    const isMissingHistoricalBest = shouldLookupCached
        && Boolean(item)
        && !hasUsableHistoricalBest
        && (!historicalLookupKey || hasCompletedHistoricalLookup);
    const hasReadonlyResult = Boolean(predictionResult) && shouldLookupCached;
    const activeJobSnapshot = jobStatus || acceptedJob;
    const activeJobProgressLabel = getJobProgressLabel(activeJobSnapshot, language);
    const isAwaitingCovariateSignature = shouldLookupCached
        && effectivePredictionType === 'mtf-pro'
        && !effectiveCovariateSignature
        && Boolean(historicalLookupKey)
        && checkedHistoricalLookupKey !== historicalLookupKey;
    const isCheckingHistoricalBest = shouldLookupCached
        && Boolean(historicalLookupKey)
        && checkedHistoricalLookupKey !== historicalLookupKey;
    const shouldShowCheckingExisting = (
        isCheckingExisting
        || isAwaitingCovariateSignature
        || isCheckingHistoricalBest
        || (Boolean(cacheLookupKey) && checkedLookupKey !== cacheLookupKey)
    ) && !predictionResult && !isSubmitting && !error;

    useLayoutEffect(() => {
        if (!isOpen || !item || !shouldLookupCached) {
            return;
        }
        if (effectivePredictionType === 'mtf-pro' && !effectiveCovariateSignature) {
            return;
        }

        setCheckedLookupKey(null);
        setIsCheckingExisting(true);
    }, [isOpen, item?.id, shouldLookupCached, effectivePredictionType, effectiveCovariateSignature]);

    useEffect(() => {
        requestSeqRef.current += 1;

        if (!isOpen) {
            return;
        }

        setHorizonLen(defaultHorizonLen);
        setContextLen(defaultContextLen);
        setSelectedPredictionType(predictionType || initialPredictionType || effectivePredictionTypeOptions[0] || 'mtf-lite');
        setIsCheckingExisting(shouldLookupCached);
        setCheckedLookupKey(null);
        setCheckedHistoricalLookupKey(null);
        setIsSubmitting(false);
        onSubmittingChange?.(false);
        setError(null);
        setAcceptedJob(null);
        setJobStatus(null);
        setPredictionResult(null);
        setFetchedHistoricalBestItem(null);
    }, [isOpen, item?.id, mode, predictionType, initialPredictionType, initialHorizonLen, initialContextLen, mtfVersion]);

    useEffect(() => {
        if (!isOpen || !item || matchingHistoricalBestItem) {
            return;
        }

        historyRequestSeqRef.current += 1;
        const requestSeq = historyRequestSeqRef.current;
        setFetchedHistoricalBestItem(null);
        setCheckedHistoricalLookupKey(null);

        (async () => {
            try {
                const response = await getAccessiblePredictions(undefined, symbol);
                if (requestSeq !== historyRequestSeqRef.current) {
                    return;
                }
                const matched = pickHistoricalBestItem(
                    flattenPublicPredictionItems(response.items || []),
                    symbol,
                    effectivePredictionType,
                    contextLen,
                    horizonLen,
                    effectiveMTFVersion,
                );
                setFetchedHistoricalBestItem(matched);
            } catch (err: any) {
                if (requestSeq !== historyRequestSeqRef.current) {
                    return;
                }
                const message = err?.message || '';
                if (onAuthError && (
                    message.includes('Authorization header required') ||
                    message.includes('401') ||
                    message.includes('Unauthorized')
                )) {
                    onAuthError();
                    return;
                }
                setFetchedHistoricalBestItem(null);
            } finally {
                if (requestSeq === historyRequestSeqRef.current) {
                    setCheckedHistoricalLookupKey(historicalLookupKey);
                }
            }
        })();
    }, [
        isOpen,
        item?.id,
        symbol,
        matchingHistoricalBestItem,
        effectivePredictionType,
        contextLen,
        horizonLen,
        effectiveMTFVersion,
        onAuthError,
        historicalLookupKey,
    ]);

    useEffect(() => {
        if (!shouldLookupCached) {
            return;
        }
        if (!isOpen || !item) {
            return;
        }
        if (effectivePredictionType === 'mtf-pro' && !effectiveCovariateSignature) {
            setIsCheckingExisting(false);
            setCheckedLookupKey(null);
            setError(null);
            return;
        }

        requestSeqRef.current += 1;
        const requestSeq = requestSeqRef.current;

        setIsCheckingExisting(true);
        setError(null);
        setAcceptedJob(null);
        setJobStatus(null);
        setPredictionResult(null);

        const lookupParams = {
            stock_code: requestStockCode,
            stock_type: stockTypeValue,
            time_step: DEFAULT_TIME_STEP,
            years: DEFAULT_YEARS,
            horizon_len: horizonLen,
            context_len: contextLen,
            prediction_type: effectivePredictionType,
            ...(effectivePredictionType === 'mtf-pro'
                ? {
                    covariate_preset: 'market_cov_v1',
                    ...(effectiveCovariateSignature ? { covariate_signature: effectiveCovariateSignature } : {}),
                }
                : {}),
        };

        (async () => {
            try {
                const cached = await mtfAPI.getPredictOnceCached(lookupParams);
                if (requestSeq !== requestSeqRef.current) {
                    return;
                }
                if (cached?.data) {
                    setPredictionResult(cached.data);
                }
            } catch (err: any) {
                if (requestSeq !== requestSeqRef.current) {
                    return;
                }
                const message = err?.message || t('singlePrediction.errorGeneric');
                if (onAuthError && (
                    message.includes('Authorization header required') ||
                    message.includes('401') ||
                    message.includes('Unauthorized')
                )) {
                    onAuthError();
                    return;
                }
                setError(null);
            } finally {
                if (requestSeq === requestSeqRef.current) {
                    setIsCheckingExisting(false);
                    setCheckedLookupKey(cacheLookupKey);
                }
            }
        })();
    }, [
        isOpen,
        item?.id,
        requestStockCode,
        stockTypeValue,
        shouldLookupCached,
        horizonLen,
        contextLen,
        effectivePredictionType,
        effectiveCovariateSignature,
        effectiveMTFVersion,
        cacheLookupKey,
    ]);

    if (!isOpen || !item) {
        return null;
    }

    const handleClose = () => {
        requestSeqRef.current += 1;
        if (!isSubmitting) {
            onClose();
            return;
        }
        onClose();
    };

    const pollJobStatus = async (jobId: string, requestSeq: number, submittedParams: typeof requestParams) => {
        for (let attempt = 0; attempt < MAX_POLL_ATTEMPTS; attempt += 1) {
            if (requestSeq !== requestSeqRef.current) {
                return;
            }
            const nextStatus = await mtfAPI.getJobStatus(jobId);
            if (requestSeq !== requestSeqRef.current) {
                return;
            }
            setJobStatus(nextStatus);

            if (nextStatus.status === 'succeeded') {
                const result = extractDirectPredictionResult(nextStatus);
                if (!result) {
                    throw new Error(t('singlePrediction.errorMissingResult'));
                }
                setPredictionResult(result);
                await onPredictionComplete?.(result, submittedParams);
                setIsSubmitting(false);
                onSubmittingChange?.(false);
                if (mode === 'next_chunk') {
                    onClose();
                }
                return;
            }

            if (nextStatus.status === 'failed') {
                throw new Error(normalizeJobError(nextStatus));
            }

            await sleep(POLL_INTERVAL_MS);
        }

        throw new Error(t('singlePrediction.errorTimeout'));
    };

    const handlePredict = async (e: React.FormEvent) => {
        e.preventDefault();
        if (shouldShowCheckingExisting) {
            return;
        }
        if (isMissingHistoricalBest) {
            setError(t('singlePrediction.errorMissingBest'));
            return;
        }
        requestSeqRef.current += 1;
        const requestSeq = requestSeqRef.current;

        setIsSubmitting(true);
        onSubmittingChange?.(true);
        setError(null);
        setAcceptedJob(null);
        setJobStatus(null);
        setPredictionResult(null);

        try {
            const submittedParams = { ...requestParams };
            const accepted = await mtfAPI.predictOnce(submittedParams);
            if (requestSeq !== requestSeqRef.current) {
                return;
            }

            setAcceptedJob(accepted);

            if (!accepted.job_id) {
                throw new Error(t('singlePrediction.errorMissingJob'));
            }

            await pollJobStatus(accepted.job_id, requestSeq, submittedParams);
        } catch (err: any) {
            if (requestSeq !== requestSeqRef.current) {
                return;
            }
            const message = err?.message || t('singlePrediction.errorGeneric');
            if (onAuthError && (
                message.includes('Authorization header required') ||
                message.includes('401') ||
                message.includes('Unauthorized')
            )) {
                onAuthError();
                return;
            }
            setError(message);
            setIsSubmitting(false);
            onSubmittingChange?.(false);
        }
    };
    const title = mode === 'next_chunk'
        ? (language === 'zh' ? 'MTF 预测' : 'MTF Prediction')
        : t('singlePrediction.title');
    const submitLabel = mode === 'next_chunk'
        ? (language === 'zh' ? '开始预测' : 'Start Prediction')
        : t('singlePrediction.submit');

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm">
            <div className="w-full max-w-7xl bg-card-dark rounded-xl shadow-2xl border border-white/10 overflow-hidden flex flex-col max-h-[90vh]">
                <div className="px-6 py-4 border-b border-white/10 flex items-center justify-between">
                    <div>
                        <h2 className="text-xl font-bold text-white">{title}</h2>
                        <p className="text-sm text-white/60 mt-1">
                            {companyName} · {symbol}
                        </p>
                    </div>
                    <button
                        onClick={handleClose}
                        className="text-white/60 hover:text-white transition-colors"
                    >
                        <span className="material-symbols-outlined">close</span>
                    </button>
                </div>

                <div className="p-6 overflow-y-auto flex-1 space-y-6">
                    <form onSubmit={handlePredict} className="grid grid-cols-1 lg:grid-cols-[1fr_1fr_1fr_auto] gap-4 items-end">
                        {effectivePredictionTypeOptions.length > 0 && (
                            <div className="space-y-3">
                                <label className="block text-sm font-medium text-white/80">
                                    {language === 'zh' ? '模型模式' : 'Model Mode'}
                                </label>
                                <div className="flex flex-wrap gap-2">
                                    {effectivePredictionTypeOptions.map(option => {
                                        const isActive = effectivePredictionType === option;
                                        const isPro = option === 'mtf-pro';
                                        const activeClass = isPro
                                            ? 'border border-amber-200/45 bg-[linear-gradient(135deg,rgba(255,241,184,0.95)_0%,rgba(252,211,77,0.95)_36%,rgba(245,158,11,0.95)_72%,rgba(249,115,22,0.95)_100%)] text-[#241400] shadow-[0_10px_28px_rgba(245,158,11,0.18)]'
                                            : 'bg-primary text-black';

                                        return (
                                            <button
                                                key={option}
                                                type="button"
                                                onClick={() => {
                                                    setCheckedLookupKey(null);
                                                    setError(null);
                                                    setPredictionResult(null);
                                                    setSelectedPredictionType(option);
                                                }}
                                                disabled={isSubmitting}
                                                className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                                                    isActive
                                                        ? activeClass
                                                        : 'bg-white/5 text-white/70 hover:bg-white/10 hover:text-white'
                                                } disabled:cursor-not-allowed disabled:opacity-60`}
                                            >
                                                {isPro
                                                    ? (language === 'zh' ? 'Pro 模型' : 'Pro Model')
                                                    : (language === 'zh' ? 'Lite 模型' : 'Lite Model')}
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>
                        )}

                        <div className="space-y-3">
                            <label className="block text-sm font-medium text-white/80">{t('singlePrediction.horizonLabel')}</label>
                            <div className="flex flex-wrap gap-2">
                                {effectiveHorizonOptions.map(option => (
                                    <button
                                        key={option}
                                        type="button"
                                        onClick={() => {
                                            setCheckedLookupKey(null);
                                            setError(null);
                                            setPredictionResult(null);
                                            setHorizonLen(option);
                                        }}
                                        disabled={isSubmitting}
                                        className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                                            horizonLen === option
                                                ? 'bg-primary text-black'
                                                : 'bg-white/5 text-white/70 hover:bg-white/10 hover:text-white'
                                        }`}
                                    >
                                        {option} {t('prediction.days')}
                                    </button>
                                ))}
                            </div>
                        </div>

                        <div className="space-y-3">
                            <label className="block text-sm font-medium text-white/80">{t('singlePrediction.contextLabel')}</label>
                            <div className="flex flex-wrap gap-2">
                                {effectiveContextOptions.map(option => (
                                    <button
                                        key={option}
                                        type="button"
                                        onClick={() => {
                                            setCheckedLookupKey(null);
                                            setError(null);
                                            setPredictionResult(null);
                                            setContextLen(option);
                                        }}
                                        disabled={isSubmitting}
                                        className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                                            contextLen === option
                                                ? 'bg-primary text-black'
                                                : 'bg-white/5 text-white/70 hover:bg-white/10 hover:text-white'
                                        }`}
                                    >
                                        {option >= 1024 ? `${Math.round(option / 1024)}K` : option}
                                    </button>
                                ))}
                            </div>
                        </div>

                        {!hasReadonlyResult && (
                            <button
                                type="submit"
                                disabled={isSubmitting || shouldShowCheckingExisting || isMissingHistoricalBest}
                                className="h-11 px-5 rounded-lg bg-primary text-black text-sm font-bold hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
                            >
                                {isCheckingExisting || shouldShowCheckingExisting ? (
                                    <>
                                        <div className="w-4 h-4 border-2 border-black/30 border-t-black rounded-full animate-spin"></div>
                                        <span>{t('singlePrediction.checking')}</span>
                                    </>
                                ) : isSubmitting ? (
                                    <>
                                        <div className="w-4 h-4 border-2 border-black/30 border-t-black rounded-full animate-spin"></div>
                                        <span>{activeJobProgressLabel}</span>
                                    </>
                                ) : (
                                    <span>{submitLabel}</span>
                                )}
                            </button>
                        )}
                    </form>

                    {error && (
                        <div className="rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-sm text-red-300">
                            {error}
                        </div>
                    )}

                    {!error && isMissingHistoricalBest && (
                        <div className="rounded-xl border border-amber-400/25 bg-amber-400/10 p-4 text-sm text-amber-200">
                            {t('singlePrediction.errorMissingBest')}
                        </div>
                    )}

                    <div className="rounded-xl border border-white/10 bg-white/5 p-4 min-h-[420px] flex flex-col">
                        {predictionResult ? (
                            <>
                                <PredictionChartPanel
                                    change={predictedChangePercent}
                                    chartData={directChartData || undefined}
                                    className="mb-4 h-[300px]"
                                    currentPrice={latestPrediction}
                                    scrollable
                                    startPrice={latestClose}
                                />

                                <div className="flex items-center justify-between mb-3">
                                    <div>
                                        <p className="text-sm font-bold text-white">{t('singlePrediction.resultTitle')}</p>
                                    </div>
                                    <div className="text-right">
                                        <p className="text-xs text-white/50">{t('singlePrediction.predictedChange')}</p>
                                        <p className={`text-sm font-bold ${predictedChangePercent >= 0 ? 'text-primary' : 'text-red-400'}`}>
                                            {predictedChangePercent >= 0 ? '+' : ''}{predictedChangePercent.toFixed(2)}%
                                        </p>
                                    </div>
                                </div>

                                <div className="grid grid-cols-1 md:grid-cols-2 gap-2 overflow-y-auto">
                                    {predictionResult.future_dates.map((date, index) => (
                                        <div key={`${date}-${index}`} className="flex items-center justify-between rounded-lg border border-white/10 bg-black/20 px-3 py-2">
                                            <span className="text-sm text-white/70">{date}</span>
                                            <span className="text-sm font-bold text-white">
                                                {numericSelectedSeries[index] != null ? numericSelectedSeries[index].toFixed(2) : '—'}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            </>
                        ) : (
                            <div className="flex-1 flex flex-col items-center justify-center text-center px-6">
                                <div className="w-12 h-12 rounded-full border border-white/10 bg-white/5 flex items-center justify-center mb-4">
                                    <span className="material-symbols-outlined text-white/60">timeline</span>
                                </div>
                                <p className="text-lg font-bold text-white mb-2">
                                    {isCheckingExisting || shouldShowCheckingExisting
                                        ? t('singlePrediction.checkingTitle')
                                        : isSubmitting
                                            ? activeJobProgressLabel
                                            : t('singlePrediction.emptyTitle')}
                                </p>
                                {!(isCheckingExisting || shouldShowCheckingExisting) && (
                                    <p className="text-sm text-white/60 max-w-md">
                                        {isSubmitting
                                            ? (language === 'zh'
                                                ? '任务已提交，正在根据后端执行阶段更新状态。'
                                                : 'The job has been submitted. Status follows the backend execution stage.')
                                            : t('singlePrediction.emptySubtitle')}
                                    </p>
                                )}
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
};

export default SinglePredictionModal;
