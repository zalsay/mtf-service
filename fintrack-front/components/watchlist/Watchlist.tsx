import React, { useState, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import {
  PredictionChartData,
  PublicPredictionItem,
  StockData,
} from "../../types";
import { useLanguage } from "../../contexts/LanguageContext";
import { getChangeColors } from "../../utils/colorUtils";
import {
  authAPI,
  getAccessiblePredictions,
  quotesAPI,
  mtfAPI,
  MTFPredictAcceptedResponse,
  MTFPredictBestRequest,
  MTFJobStatusResponse,
  MTFPredictOnceRequest,
  DirectPredictionResult,
  watchlistAPI,
  WatchlistItem,
} from "../../services/apiService";
import {
  flattenPublicPredictionItems,
  isMTFProPredictionItem,
  mapResolvedPredictionToStockData,
  ResolvedPublicPrediction,
} from "../../utils/predictionUtils";
import AddStockModal from "../dashboard/AddStockModal";
import StockPredictionCard, {
  StockPredictionSummary,
} from "../dashboard/StockPredictionCard";
import ConfirmModal from "../common/ConfirmModal";
import SinglePredictionModal from "./SinglePredictionModal";
import type { PredictionChartMode } from "../common/PredictionChart";

interface WatchlistProps {
  initialStocks: StockData[];
  onAuthError?: () => void;
}

const Sparkline: React.FC<{ change: number }> = ({ change }) => {
  const isPositive = change >= 0;
  const src = isPositive
    ? "images/watchlist/sparkline-up.png"
    : "images/watchlist/sparkline-down.png";
  return (
    <img
      className="h-8 w-full object-contain"
      alt={isPositive ? "Upward trend sparkline" : "Downward trend sparkline"}
      src={src}
    />
  );
};

const normalizePredictionSymbol = (symbol: string): string => {
  const trimmed = String(symbol || "")
    .trim()
    .toLowerCase();
  const digits = trimmed.replace(/\D/g, "");
  return digits || trimmed;
};

type TrainPredictionType = "mtf-lite" | "mtf-pro";
type ChartModelGroup = "lite_pro" | "lite" | "pro";

interface MTFTrainPolicy {
  predictionTypes: TrainPredictionType[];
  contextLens: number[];
  horizonLens: number[];
  defaultPredictionType: TrainPredictionType;
  defaultContextLen: number;
  defaultHorizonLen: number;
}

interface TrainResultPreference {
  predictionType: TrainPredictionType;
  contextLen: number;
  horizonLen: number;
  mtfVersion: string;
  covariateSignature?: string;
}

interface TrainingProgressState {
  estimatedSeconds: number;
  startedAt: number;
}

interface ChartPredictionOption {
  id: string;
  modelGroup: ChartModelGroup;
  modelLabel: string;
  horizonLen: number;
  contextLen: number;
  contextLabel: string;
  mtfVersion: string;
  resolved: ResolvedPublicPrediction;
}

const DEFAULT_MTF_YEARS = 15;
const DEFAULT_MTF_VERSION = "2.5";
const CHART_HORIZON_CONTROLS = [7, 14, 28] as const;
const CHART_CONTEXT_CONTROLS = [512, 1024, 2048] as const;
const BEST_TRAIN_POLL_INTERVAL_MS = 2000;
const BEST_TRAIN_MAX_POLL_ATTEMPTS = 180;
const PRO_PROGRESS_GRADIENT =
  "linear-gradient(90deg, #FFF1B8 0%, #FCD34D 24%, #F59E0B 52%, #FB923C 78%, #F97316 100%)";

const sleep = (ms: number) =>
  new Promise((resolve) => window.setTimeout(resolve, ms));

const normalizeEstimatedSeconds = (seconds: unknown): number | null => {
  const value = Number(seconds);
  if (!Number.isFinite(value) || value <= 0) {
    return null;
  }
  return value;
};

const getMTFTrainPolicy = (membershipLevel: number): MTFTrainPolicy => {
  switch (membershipLevel) {
    case 3:
      return {
        predictionTypes: ["mtf-lite", "mtf-pro"],
        contextLens: [512, 1024, 2048],
        horizonLens: [7, 14, 28],
        defaultPredictionType: "mtf-pro",
        defaultContextLen: 2048,
        defaultHorizonLen: 7,
      };
    case 2:
      return {
        predictionTypes: ["mtf-lite", "mtf-pro"],
        contextLens: [512, 1024],
        horizonLens: [7, 14, 28],
        defaultPredictionType: "mtf-pro",
        defaultContextLen: 1024,
        defaultHorizonLen: 7,
      };
    case 1:
      return {
        predictionTypes: ["mtf-lite", "mtf-pro"],
        contextLens: [512],
        horizonLens: [7, 14, 28],
        defaultPredictionType: "mtf-pro",
        defaultContextLen: 512,
        defaultHorizonLen: 7,
      };
    default:
      return {
        predictionTypes: ["mtf-lite"],
        contextLens: [512],
        horizonLens: [7],
        defaultPredictionType: "mtf-lite",
        defaultContextLen: 512,
        defaultHorizonLen: 7,
      };
  }
};

const isWatchlistItemOverLimit = (
  item: WatchlistItem | null | undefined,
): boolean => {
  return !!item?.is_over_limit;
};

const getTrainingStockCode = (item: WatchlistItem | null): string => {
  const symbol = String(item?.stock?.symbol || "")
    .trim()
    .toLowerCase();
  if (!symbol) {
    return "";
  }
  const stockType = item?.stock_type || 1;
  return stockType === 2 ? symbol : symbol.replace(/^(sh|sz)/, "");
};

const normalizeMTFJobError = (error: unknown): string => {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "MTF model training failed";
};

const normalizeOptionalText = (value: unknown): string =>
  String(value || "")
    .trim()
    .toLowerCase();

const matchesTrainResultPreference = (
  resolved: ResolvedPublicPrediction,
  preference?: TrainResultPreference | null,
): boolean => {
  if (!preference) {
    return true;
  }

  const candidate =
    preference.predictionType === "mtf-pro"
      ? resolved.pro ||
        (normalizeOptionalText(resolved.primary.best.prediction_type) ===
        "mtf-pro"
          ? resolved.primary
          : undefined)
      : normalizeOptionalText(resolved.primary.best.prediction_type) ===
          "mtf-pro"
        ? undefined
        : resolved.primary;

  if (!candidate) {
    return false;
  }

  if (Number(candidate.best.context_len || 0) !== preference.contextLen) {
    return false;
  }
  if (Number(candidate.best.horizon_len || 0) !== preference.horizonLen) {
    return false;
  }
  if (
    String(candidate.best.mtf_version || "").trim() !== preference.mtfVersion
  ) {
    return false;
  }

  if (
    preference.predictionType === "mtf-pro" &&
    preference.covariateSignature
  ) {
    return (
      normalizeOptionalText(candidate.best.covariate_signature) ===
      normalizeOptionalText(preference.covariateSignature)
    );
  }

  return true;
};

const isMTFProPredictionItemLocal = (
  item?: PublicPredictionItem | null,
): boolean => {
  return isMTFProPredictionItem(item);
};

const getPredictionItemForType = (
  resolved: ResolvedPublicPrediction | null,
  predictionType: TrainPredictionType,
): PublicPredictionItem | null => {
  if (!resolved) {
    return null;
  }
  if (predictionType === "mtf-pro") {
    if (resolved.pro) {
      return resolved.pro;
    }
    return isMTFProPredictionItemLocal(resolved.primary)
      ? resolved.primary
      : null;
  }
  return isMTFProPredictionItemLocal(resolved.primary)
    ? null
    : resolved.primary;
};

const getLoadedPredictionType = (
  resolved: ResolvedPublicPrediction,
): TrainPredictionType =>
  isMTFProPredictionItemLocal(resolved.primary) && !resolved.pro
    ? "mtf-pro"
    : "mtf-lite";

const comparePredictionUpdatedAt = (left?: string, right?: string): number => {
  const leftTime = left ? new Date(left).getTime() : 0;
  const rightTime = right ? new Date(right).getTime() : 0;
  return leftTime - rightTime;
};

const selectLatestPredictionItem = (
  current: PublicPredictionItem | undefined,
  next: PublicPredictionItem,
): PublicPredictionItem => {
  if (!current) {
    return next;
  }
  return comparePredictionUpdatedAt(
    current.best.updated_at,
    next.best.updated_at,
  ) <= 0
    ? next
    : current;
};

const formatChartContextLabel = (contextLen?: number): string => {
  const value = Number(contextLen || 0);
  if (!value) {
    return "?";
  }
  return value < 1024 ? String(value) : `${Math.round(value / 1024)}K`;
};

const getChartModelMeta = (
  resolved: ResolvedPublicPrediction,
  language: string,
): { group: ChartModelGroup; label: string } => {
  const primaryIsPro = isMTFProPredictionItemLocal(resolved.primary);
  const hasLite = !primaryIsPro;
  const hasPro = Boolean(resolved.pro) || primaryIsPro;

  if (hasLite && hasPro) {
    return {
      group: "lite_pro",
      label: language === "zh" ? "Lite + Pro" : "Lite + Pro",
    };
  }
  if (hasPro) {
    return {
      group: "pro",
      label: language === "zh" ? "Pro 模型" : "Pro Model",
    };
  }
  return {
    group: "lite",
    label: language === "zh" ? "Lite 模型" : "Lite Model",
  };
};

const createChartPredictionOption = (
  key: string,
  resolved: ResolvedPublicPrediction,
  language: string,
): ChartPredictionOption => {
  const modelMeta = getChartModelMeta(resolved, language);
  const baseItem =
    getPredictionItemForType(
      resolved,
      modelMeta.group === "pro" ? "mtf-pro" : "mtf-lite",
    ) || resolved.primary;
  const horizonLen = Number(baseItem.best.horizon_len || 0);
  const contextLen = Number(baseItem.best.context_len || 0);
  const contextLabel = formatChartContextLabel(contextLen);
  const mtfVersion =
    String(baseItem.best.mtf_version || DEFAULT_MTF_VERSION).trim() ||
    DEFAULT_MTF_VERSION;

  return {
    id: key,
    modelGroup: modelMeta.group,
    modelLabel: modelMeta.label,
    horizonLen,
    contextLen,
    contextLabel,
    mtfVersion,
    resolved,
  };
};

const getChartModelSortWeight = (group: ChartModelGroup): number => {
  if (group === "lite_pro") {
    return 0;
  }
  if (group === "lite") {
    return 1;
  }
  return 2;
};

const formatChartHorizonLabel = (
  horizonLen: number,
  language: string,
): string =>
  horizonLen ? `${horizonLen}${language === "zh" ? "天" : "d"}` : "?";

const getUniqueChartOptions = <T extends string | number>(
  options: ChartPredictionOption[],
  getKey: (option: ChartPredictionOption) => T,
): ChartPredictionOption[] => {
  const mapped = new Map<T, ChartPredictionOption>();
  options.forEach((option) => {
    const key = getKey(option);
    if (!mapped.has(key)) {
      mapped.set(key, option);
    }
  });
  return Array.from(mapped.values());
};

const pickClosestChartOption = (
  options: ChartPredictionOption[],
  current: ChartPredictionOption | undefined,
  patch: Partial<
    Pick<
      ChartPredictionOption,
      "modelGroup" | "horizonLen" | "contextLen" | "mtfVersion"
    >
  >,
): ChartPredictionOption | undefined => {
  const candidates = options.filter(
    (option) =>
      (patch.modelGroup === undefined ||
        option.modelGroup === patch.modelGroup) &&
      (patch.horizonLen === undefined ||
        option.horizonLen === patch.horizonLen) &&
      (patch.contextLen === undefined ||
        option.contextLen === patch.contextLen) &&
      (patch.mtfVersion === undefined ||
        option.mtfVersion === patch.mtfVersion),
  );
  if (!candidates.length) {
    return undefined;
  }
  if (!current) {
    return candidates[0];
  }

  return candidates
    .map((option) => {
      const score = [
        option.id === current.id ? 64 : 0,
        option.modelGroup === current.modelGroup ? 32 : 0,
        option.horizonLen === current.horizonLen ? 16 : 0,
        option.contextLen === current.contextLen ? 8 : 0,
        option.mtfVersion === current.mtfVersion ? 4 : 0,
      ].reduce((sum, value) => sum + value, 0);
      return { option, score };
    })
    .sort((left, right) => right.score - left.score)[0]?.option;
};

const buildChartParameterButtonClass = (
  active: boolean,
  proStyle = false,
): string => {
  if (active && proStyle) {
    return "border-amber-200/45 bg-[linear-gradient(135deg,rgba(255,241,184,0.95)_0%,rgba(252,211,77,0.95)_36%,rgba(245,158,11,0.95)_72%,rgba(249,115,22,0.95)_100%)] text-[#241400] shadow-[0_10px_28px_rgba(245,158,11,0.18)]";
  }
  return active
    ? "border-primary bg-primary text-black shadow-sm"
    : "border-white/10 bg-white/[0.05] text-white/70 hover:border-white/18 hover:bg-white/[0.09] hover:text-white";
};

const renderChartParameterGroup = (
  label: string,
  children: React.ReactNode,
): React.ReactElement => (
  <div className="space-y-2">
    <div className="shrink-0 text-xs font-semibold text-white/50">{label}</div>
    <div className="flex flex-wrap gap-2">{children}</div>
  </div>
);

const renderChartParameterButton = (
  key: string,
  label: string,
  active: boolean,
  onClick: () => void,
  disabled: boolean,
  proStyle = false,
): React.ReactElement => (
  <button
    key={key}
    type="button"
    onClick={onClick}
    disabled={disabled}
    className={`min-h-9 whitespace-nowrap rounded-lg border px-3 py-1.5 text-xs font-bold transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${buildChartParameterButtonClass(active, proStyle)}`}
  >
    {label}
  </button>
);

const buildChartPredictionOptions = (
  items: PublicPredictionItem[],
  normalizedSymbol: string,
  language: string,
): ChartPredictionOption[] => {
  const grouped = new Map<
    string,
    { primary?: PublicPredictionItem; pro?: PublicPredictionItem }
  >();

  for (const item of items || []) {
    if (!item?.best?.symbol || !item.best.best_prediction_item) {
      continue;
    }
    if (normalizePredictionSymbol(item.best.symbol) !== normalizedSymbol) {
      continue;
    }

    const key = [
      normalizePredictionSymbol(item.best.symbol),
      item.best.horizon_len ?? "",
      item.best.context_len ?? "",
      item.best.mtf_version ?? "",
    ].join("|");
    const bucket = grouped.get(key) || {};
    if (isMTFProPredictionItemLocal(item)) {
      bucket.pro = selectLatestPredictionItem(bucket.pro, item);
    } else {
      bucket.primary = selectLatestPredictionItem(bucket.primary, item);
    }
    grouped.set(key, bucket);
  }

  return Array.from(grouped.entries())
    .map(([key, bucket]): ChartPredictionOption | null => {
      const primary = bucket.primary || bucket.pro;
      if (!primary) {
        return null;
      }
      const resolved: ResolvedPublicPrediction = {
        primary,
        pro: bucket.primary ? bucket.pro : undefined,
      };
      const previewType =
        isMTFProPredictionItemLocal(resolved.primary) && !resolved.pro
          ? "mtf-pro"
          : undefined;
      if (
        !mapResolvedPredictionToStockData(resolved, language, previewType)
          ?.prediction?.chartData
      ) {
        return null;
      }
      return createChartPredictionOption(key, resolved, language);
    })
    .filter((option): option is ChartPredictionOption => option !== null)
    .sort((left, right) => {
      if (left.horizonLen !== right.horizonLen) {
        return left.horizonLen - right.horizonLen;
      }
      if (left.contextLen !== right.contextLen) {
        return left.contextLen - right.contextLen;
      }
      const modelDiff =
        getChartModelSortWeight(left.modelGroup) -
        getChartModelSortWeight(right.modelGroup);
      if (modelDiff !== 0) {
        return modelDiff;
      }
      return left.mtfVersion.localeCompare(right.mtfVersion);
    });
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

  const rawPreferredValues =
    Number(result.stock_type || 0) === 1 &&
    preferredKey &&
    Array.isArray(result.adjust_raw_predictions?.[preferredKey])
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

  const preferredValues =
    preferredKey && Array.isArray(result.predictions?.[preferredKey])
      ? result.predictions[preferredKey]
      : [];
  if (preferredValues.length > 0) {
    return preferredValues.map(Number).filter(Number.isFinite);
  }

  const firstValues =
    Object.values(result.predictions || {}).find(Array.isArray) || [];
  return firstValues.map(Number).filter(Number.isFinite);
};

const getDirectLatestClose = (result: DirectPredictionResult): number => {
  const rawLatestClose = Number(result.adjust_raw_latest_close);
  if (Number(result.stock_type || 0) === 1 && rawLatestClose > 0) {
    return rawLatestClose;
  }
  return Number(result.latest_close);
};

const getChunkPredictionSeries = (
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

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const toFiniteNumberArray = (value: unknown): number[] =>
  Array.isArray(value) ? value.map(Number).filter(Number.isFinite) : [];

const toPredictionMap = (
  value: unknown,
): Record<string, number[]> | undefined => {
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

const currentPriceChunkView = (
  chunk: PublicPredictionItem["chunks"][number],
) => {
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
  const rawDates = Array.isArray(raw.dates)
    ? raw.dates.map(String).filter(Boolean)
    : [];
  return {
    ...chunk,
    predictions: toPredictionMap(raw.predictions) || chunk.predictions,
    actual_values: rawActuals.length > 0 ? rawActuals : chunk.actual_values,
    predicted_change_percent:
      toPredictionMap(raw.predicted_change_percent) ||
      chunk.predicted_change_percent,
    actual_change_percent:
      toFiniteNumberArray(raw.actual_change_percent).length > 0
        ? toFiniteNumberArray(raw.actual_change_percent)
        : chunk.actual_change_percent,
    change_base_value: Number.isFinite(Number(raw.change_base_value))
      ? Number(raw.change_base_value)
      : chunk.change_base_value,
    change_base_date:
      typeof raw.change_base_date === "string"
        ? raw.change_base_date
        : chunk.change_base_date,
    dates: rawDates.length > 0 ? rawDates : chunk.dates,
  };
};

const parseChunkDateTime = (value: unknown): number => {
  const raw = String(value || "").trim();
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
  chunks: PublicPredictionItem["chunks"],
  bestKey: string,
  latestDataDate?: string | null,
) => {
  const latestDataTime = parseChunkDateTime(latestDataDate);
  const sortedChunks = [...(chunks || [])].sort(
    (a, b) =>
      parseChunkDateTime(a.start_date) - parseChunkDateTime(b.start_date),
  );

  const hasDrawableHistory = (
    sourceChunk: PublicPredictionItem["chunks"][number],
  ) => {
    const chunk = currentPriceChunkView(sourceChunk);
    const chunkPredictions = getChunkPredictionSeries(
      chunk.predictions,
      bestKey,
    );
    return (
      (chunk.dates?.length || 0) > 0 &&
      (chunk.actual_values?.length || 0) > 0 &&
      chunkPredictions.length > 0
    );
  };

  const historicalChunks = sortedChunks.filter((chunk) => {
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
  directResult: DirectPredictionResult,
  dates: string[],
  actuals: number[],
  predictions: number[],
  actualChangePercents: number[],
  predictedChangePercents: number[],
) => {
  const latestDate = String(
    directResult.latest_data_date || directResult.request_end_date || "",
  ).trim();
  const latestClose = getDirectLatestClose(directResult);
  if (!latestDate || !(latestClose > 0)) {
    return;
  }

  const lastDate = dates[dates.length - 1];
  if (
    lastDate &&
    parseChunkDateTime(latestDate) <= parseChunkDateTime(lastDate)
  ) {
    return;
  }

  dates.push(latestDate);
  actuals.push(latestClose);
  predictions.push(0);
  actualChangePercents.push(Number.NaN);
  predictedChangePercents.push(Number.NaN);
};

const buildNextChunkChartData = (
  bestItem: PublicPredictionItem,
  directResult: DirectPredictionResult,
  predictionType: TrainPredictionType,
): PredictionChartData | null => {
  const bestKey = bestItem.best.best_prediction_item;
  const lastChunk = pickLatestHistoricalChunk(
    bestItem.chunks || [],
    bestKey,
    directResult.latest_data_date || directResult.request_end_date,
  );
  if (!lastChunk || !bestKey) {
    return null;
  }

  const currentPriceChunk = currentPriceChunkView(lastChunk);
  const chunkDates = Array.isArray(currentPriceChunk.dates)
    ? currentPriceChunk.dates
    : [];
  const chunkPredictions = getChunkPredictionSeries(
    currentPriceChunk.predictions,
    bestKey,
  );
  const historyLen = Math.min(
    chunkDates.length,
    Math.max(
      currentPriceChunk.actual_values?.length || 0,
      chunkPredictions.length,
    ),
  );

  const dates = chunkDates.slice(0, historyLen).map(String);
  const actuals = Array.from({ length: historyLen }, (_, index) => {
    const num = Number(currentPriceChunk.actual_values?.[index]);
    return Number.isFinite(num) ? num : 0;
  });
  const predictions = Array.from({ length: historyLen }, (_, index) => {
    const num = Number(chunkPredictions[index]);
    return Number.isFinite(num) ? num : 0;
  });
  const actualChangePercents = Array.isArray(
    currentPriceChunk.actual_change_percent,
  )
    ? currentPriceChunk.actual_change_percent.slice(0, historyLen).map(Number)
    : [];
  const chunkPredictedChangePercents = getChunkPredictionSeries(
    currentPriceChunk.predicted_change_percent,
    bestKey,
  );
  const predictedChangePercents = Array.from(
    { length: historyLen },
    (_, index) => {
      const num = Number(chunkPredictedChangePercents[index]);
      return Number.isFinite(num) ? num : Number.NaN;
    },
  );

  const futureDates = (directResult.future_dates || [])
    .map(String)
    .filter(Boolean);
  const futurePredictions = getDirectPredictionValues(directResult, bestKey);
  const directPredictedChangePercents = getChunkPredictionSeries(
    directResult.predicted_change_percent,
    bestKey,
  );
  const futureLen = Math.min(futureDates.length, futurePredictions.length);
  if (futureLen === 0) {
    return null;
  }

  const changeBase =
    Number(directResult.change_base_value) ||
    actuals[actuals.length - 1] ||
    Number(currentPriceChunk.change_base_value) ||
    getDirectLatestClose(directResult) ||
    0;

  appendLatestActualAnchor(
    directResult,
    dates,
    actuals,
    predictions,
    actualChangePercents,
    predictedChangePercents,
  );

  for (let index = 0; index < futureLen; index += 1) {
    const predicted = Number(futurePredictions[index]);
    dates.push(futureDates[index]);
    predictions.push(Number.isFinite(predicted) ? predicted : 0);
    const backendPredictedChange = Number(directPredictedChangePercents[index]);
    predictedChangePercents.push(
      Number.isFinite(backendPredictedChange)
        ? backendPredictedChange
        : changeBase > 0 && Number.isFinite(predicted)
          ? ((predicted - changeBase) / changeBase) * 100
          : Number.NaN,
    );
  }

  if (predictionType === "mtf-pro") {
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

interface TrainSelectOption<T extends string | number> {
  value: T;
  label: string;
}

interface CustomTrainSelectProps<T extends string | number> {
  label: string;
  value: T;
  options: TrainSelectOption<T>[];
  disabled?: boolean;
  onChange: (value: T) => void;
}

const CustomTrainSelect = <T extends string | number>({
  label,
  value,
  options,
  disabled = false,
  onChange,
}: CustomTrainSelectProps<T>) => {
  const [open, setOpen] = useState(false);
  const [menuPosition, setMenuPosition] = useState({
    left: 0,
    top: 0,
    width: 0,
    maxHeight: 240,
  });
  const containerRef = useRef<HTMLDivElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  const updateMenuPosition = () => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) {
      return;
    }

    const gap = 8;
    const viewportPadding = 12;
    const preferredMaxHeight = 240;
    const belowSpace = window.innerHeight - rect.bottom - viewportPadding;
    const aboveSpace = rect.top - viewportPadding;
    const shouldOpenUp = belowSpace < 160 && aboveSpace > belowSpace;
    const availableSpace = shouldOpenUp ? aboveSpace : belowSpace;
    const maxHeight = Math.min(
      preferredMaxHeight,
      Math.max(140, availableSpace - gap),
    );

    setMenuPosition({
      left: rect.left,
      top: shouldOpenUp ? rect.top - maxHeight - gap : rect.bottom + gap,
      width: rect.width,
      maxHeight,
    });
  };

  useEffect(() => {
    if (!open) {
      return;
    }

    updateMenuPosition();

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (
        !containerRef.current?.contains(target) &&
        !menuRef.current?.contains(target)
      ) {
        setOpen(false);
      }
    };
    const handleViewportChange = () => updateMenuPosition();

    document.addEventListener("mousedown", handlePointerDown);
    window.addEventListener("resize", handleViewportChange);
    window.addEventListener("scroll", handleViewportChange, true);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      window.removeEventListener("resize", handleViewportChange);
      window.removeEventListener("scroll", handleViewportChange, true);
    };
  }, [open]);

  const selectedOption =
    options.find((option) => option.value === value) || options[0];

  return (
    <div ref={containerRef} className="relative space-y-2">
      <label className="block text-xs font-semibold uppercase tracking-[0.16em] text-white/40">
        {label}
      </label>
      <button
        type="button"
        onClick={() => {
          if (!disabled) {
            setOpen((current) => !current);
          }
        }}
        disabled={disabled}
        className={`flex h-11 w-full items-center justify-between rounded-2xl border border-white/10 bg-white/[0.04] px-3 text-left text-sm font-medium text-white transition-colors ${
          disabled
            ? "cursor-not-allowed opacity-60"
            : "cursor-pointer hover:border-white/20 hover:bg-white/[0.06]"
        } ${open ? "border-primary/40 bg-white/[0.07]" : ""}`}
      >
        <span className="truncate">{selectedOption?.label || ""}</span>
        <span
          className={`material-symbols-outlined text-[18px] text-white/55 transition-transform ${open ? "rotate-180 text-primary" : ""}`}
        >
          expand_more
        </span>
      </button>

      {open &&
        !disabled &&
        createPortal(
          <div
            ref={menuRef}
            className="fixed z-[100] overflow-y-auto rounded-2xl border border-white/10 bg-[#171717] p-1 shadow-[0_24px_60px_rgba(0,0,0,0.55)] backdrop-blur-xl"
            style={{
              left: menuPosition.left,
              top: menuPosition.top,
              width: menuPosition.width,
              maxHeight: menuPosition.maxHeight,
            }}
          >
            {options.map((option) => {
              const active = option.value === value;
              return (
                <button
                  key={String(option.value)}
                  type="button"
                  onClick={() => {
                    onChange(option.value);
                    setOpen(false);
                  }}
                  className={`flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-sm transition-colors ${
                    active
                      ? "bg-primary/12 text-primary"
                      : "text-white/78 hover:bg-white/[0.06] hover:text-white"
                  }`}
                >
                  <span>{option.label}</span>
                  {active && (
                    <span className="material-symbols-outlined text-[16px]">
                      check
                    </span>
                  )}
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </div>
  );
};

const Watchlist: React.FC<WatchlistProps> = ({
  initialStocks,
  onAuthError,
}) => {
  const { t, language } = useLanguage();
  const [watchlistItems, setWatchlistItems] = useState<WatchlistItem[]>([]);
  const [activeTab, setActiveTab] = useState<1 | 2>(1);
  const [searchTerm, setSearchTerm] = useState("");
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [stockToDelete, setStockToDelete] = useState<{
    id: number;
    symbol: string;
  } | null>(null);
  const [singlePredictionItem, setSinglePredictionItem] =
    useState<WatchlistItem | null>(null);
  const [singlePredictionMode, setSinglePredictionMode] = useState<
    "standalone" | "next_chunk"
  >("standalone");
  const [nextChunkBestItem, setNextChunkBestItem] =
    useState<PublicPredictionItem | null>(null);
  const [selectedStock, setSelectedStock] = useState<StockData | null>(null);
  const [selectedWatchlistItem, setSelectedWatchlistItem] =
    useState<WatchlistItem | null>(null);
  const [chartOpen, setChartOpen] = useState(false);
  const [chartModalMode, setChartModalMode] =
    useState<PredictionChartMode>("price");
  const [isLoading, setIsLoading] = useState(false);
  const [isChartLoading, setIsChartLoading] = useState(false);
  const [isTrainingBest, setIsTrainingBest] = useState(false);
  const [isPredictingNextChunk, setIsPredictingNextChunk] = useState(false);
  const [showTrainPanel, setShowTrainPanel] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [chartError, setChartError] = useState<string | null>(null);
  const [nextChunkChartData, setNextChunkChartData] =
    useState<PredictionChartData | null>(null);
  const [latestQuotes, setLatestQuotes] = useState<
    Record<
      string,
      {
        latest_price?: number;
        change_percent?: number;
        trading_date?: string;
        turnover_rate?: number;
      }
    >
  >({});
  const [membershipLevel, setMembershipLevel] = useState(0);
  const [trainPredictionType, setTrainPredictionType] =
    useState<TrainPredictionType>("mtf-lite");
  const [trainContextLen, setTrainContextLen] = useState(512);
  const [trainHorizonLen, setTrainHorizonLen] = useState(7);
  const [trainingJobId, setTrainingJobId] = useState<string | null>(null);
  const [trainingProgress, setTrainingProgress] =
    useState<TrainingProgressState | null>(null);
  const [trainingProgressNow, setTrainingProgressNow] = useState(Date.now());
  const [selectedResolvedPrediction, setSelectedResolvedPrediction] =
    useState<ResolvedPublicPrediction | null>(null);
  const [chartPredictionOptions, setChartPredictionOptions] = useState<
    ChartPredictionOption[]
  >([]);
  const [activeChartPredictionOptionId, setActiveChartPredictionOptionId] =
    useState<string | null>(null);
  const trainResultPreferenceRef = useRef<TrainResultPreference | null>(null);
  const chartRequestSeqRef = useRef(0);

  const getItemSymbol = (item: WatchlistItem | null | undefined) =>
    item?.stock?.symbol || "";
  const getItemCompanyName = (item: WatchlistItem | null | undefined) =>
    item?.stock?.company_name || "";
  const trainPolicy = getMTFTrainPolicy(membershipLevel);
  const trainPredictionTypeOptions: TrainSelectOption<TrainPredictionType>[] =
    trainPolicy.predictionTypes.map((option) => ({
      value: option,
      label:
        option === "mtf-pro"
          ? language === "zh"
            ? "Pro 模型"
            : "Pro Model"
          : language === "zh"
            ? "Lite 模型"
            : "Lite Model",
    }));
  const trainContextOptions: TrainSelectOption<number>[] =
    trainPolicy.contextLens.map((option) => ({
      value: option,
      label: String(option),
    }));
  const trainHorizonOptions: TrainSelectOption<number>[] =
    trainPolicy.horizonLens.map((option) => ({
      value: option,
      label: `${option}${language === "zh" ? "天" : "d"}`,
    }));
  const activeChartPredictionOption = chartPredictionOptions.find(
    (option) => option.id === activeChartPredictionOptionId,
  );
  const chartHorizonParameterOptions = getUniqueChartOptions(
    chartPredictionOptions,
    (option) => option.horizonLen,
  ).sort((left, right) => left.horizonLen - right.horizonLen);
  const chartContextParameterOptions = getUniqueChartOptions(
    chartPredictionOptions,
    (option) => option.contextLen,
  ).sort((left, right) => left.contextLen - right.contextLen);
  const chartVersionParameterOptions = getUniqueChartOptions(
    chartPredictionOptions,
    (option) => option.mtfVersion,
  ).sort((left, right) => left.mtfVersion.localeCompare(right.mtfVersion));
  const chartAvailableHorizons = new Set(
    chartHorizonParameterOptions.map((option) => option.horizonLen),
  );
  const chartAvailableContexts = new Set(
    chartContextParameterOptions.map((option) => option.contextLen),
  );
  const chartSummaryHorizonOptions = CHART_HORIZON_CONTROLS.map((value) => ({
    value,
    available: chartAvailableHorizons.has(value),
  }));
  const chartSummaryContextOptions = CHART_CONTEXT_CONTROLS.map((value) => ({
    value,
    available: chartAvailableContexts.has(value),
  }));
  const openStandalonePredictionModal = (item: WatchlistItem) => {
    if (isWatchlistItemOverLimit(item)) {
      setError(
        language === "zh"
          ? "超出当前会员等级上限。删除其他关注或升级会员后可继续预测。"
          : "Over current membership limit. Delete other items or upgrade to enable predictions.",
      );
      return;
    }
    setSinglePredictionMode("standalone");
    setNextChunkBestItem(null);
    setSinglePredictionItem(item);
  };

  const closeSinglePredictionModal = () => {
    setSinglePredictionItem(null);
    setSinglePredictionMode("standalone");
    setNextChunkBestItem(null);
    setIsPredictingNextChunk(false);
  };

  useEffect(() => {
    void loadWatchlist();
    void loadMembershipProfile();
  }, []);

  useEffect(() => {
    if (!chartOpen || !!selectedStock?.prediction || isTrainingBest) {
      return;
    }
    const policy = getMTFTrainPolicy(membershipLevel);
    setTrainPredictionType(policy.defaultPredictionType);
    setTrainContextLen(policy.defaultContextLen);
    setTrainHorizonLen(policy.defaultHorizonLen);
  }, [chartOpen, membershipLevel, selectedStock?.prediction, isTrainingBest]);

  useEffect(() => {
    if (!isTrainingBest || !trainingProgress) {
      return;
    }

    setTrainingProgressNow(Date.now());
    const timerId = window.setInterval(() => {
      setTrainingProgressNow(Date.now());
    }, 500);

    return () => window.clearInterval(timerId);
  }, [isTrainingBest, trainingProgress]);

  const loadMembershipProfile = async () => {
    try {
      const profile = await authAPI.getProfile();
      const level = Number(profile?.membership_level ?? 0);
      const normalizedLevel = Number.isFinite(level) ? level : 0;
      setMembershipLevel(normalizedLevel);
      const policy = getMTFTrainPolicy(normalizedLevel);
      setTrainPredictionType(policy.defaultPredictionType);
      setTrainContextLen(policy.defaultContextLen);
      setTrainHorizonLen(policy.defaultHorizonLen);
    } catch (err: any) {
      const message = err?.message || "";
      if (
        onAuthError &&
        (message.includes("Authorization header required") ||
          message.includes("401") ||
          message.includes("Unauthorized"))
      ) {
        onAuthError();
      }
    }
  };

  const loadWatchlist = async () => {
    try {
      setIsLoading(true);
      setError(null);
      const response = await watchlistAPI.getWatchlist();
      const validWatchlistItems = (response.watchlist || []).filter(
        (item: WatchlistItem | null | undefined): item is WatchlistItem =>
          !!item && !!item.stock?.symbol,
      );
      setWatchlistItems(validWatchlistItems);
      const quoteItems = validWatchlistItems
        .map((it: WatchlistItem) => ({
          symbol: getItemSymbol(it),
          stock_type: it.stock_type || 1,
        }))
        .filter((item) => !!item.symbol);
      if (quoteItems.length > 0) {
        try {
          const res = await quotesAPI.batchLatest(quoteItems);
          const map: Record<
            string,
            {
              latest_price?: number;
              change_percent?: number;
              trading_date?: string;
              turnover_rate?: number;
            }
          > = {};
          for (const q of res.quotes || []) {
            map[q.symbol] = {
              latest_price: q.latest_price,
              change_percent: q.change_percent,
              trading_date: q.trading_date,
              turnover_rate: q.turnover_rate,
            };
          }
          setLatestQuotes(map);
        } catch (e) {}
      } else {
        setLatestQuotes({});
      }
    } catch (err: any) {
      if (
        onAuthError &&
        err.message &&
        (err.message.includes("Authorization header required") ||
          err.message.includes("401") ||
          err.message.includes("Unauthorized"))
      ) {
        onAuthError();
      } else {
        setError(err.message);
      }
    } finally {
      setIsLoading(false);
    }
  };

  const handleAddStock = async (symbol: string, type: 1 | 2) => {
    try {
      const symLower = symbol.toLowerCase();
      const exists = watchlistItems.some(
        (it) => getItemSymbol(it).toLowerCase() === symLower,
      );
      if (exists) {
        throw new Error("duplicate symbol");
      }

      await watchlistAPI.addToWatchlist({
        symbol: symbol.toUpperCase(),
        stock_type: type,
      });
      await loadWatchlist();
    } catch (err: any) {
      if (
        onAuthError &&
        err.message &&
        (err.message.includes("Authorization header required") ||
          err.message.includes("401") ||
          err.message.includes("Unauthorized"))
      ) {
        onAuthError();
        return;
      }
      throw err;
    }
  };

  const confirmRemove = (id: number) => {
    const sym = getItemSymbol(
      watchlistItems.find((it) => it?.id === id),
    ).toLowerCase();
    setStockToDelete({ id, symbol: sym });
    setDeleteModalOpen(true);
  };

  const handleRemoveStock = async () => {
    if (!stockToDelete) return;

    try {
      setIsLoading(true);
      await watchlistAPI.removeFromWatchlist(stockToDelete.id);
      await loadWatchlist();
      setDeleteModalOpen(false);
      setStockToDelete(null);
    } catch (err: any) {
      if (
        onAuthError &&
        err.message &&
        (err.message.includes("Authorization header required") ||
          err.message.includes("401") ||
          err.message.includes("Unauthorized"))
      ) {
        onAuthError();
      } else {
        setError(err.message);
      }
    } finally {
      setIsLoading(false);
    }
  };

  const applyChartPredictionOption = (
    option: ChartPredictionOption,
    item: WatchlistItem,
    preferredPredictionType?: TrainPredictionType,
  ) => {
    setSelectedResolvedPrediction(option.resolved);
    setActiveChartPredictionOptionId(option.id);

    const loadedPredictionType =
      preferredPredictionType || getLoadedPredictionType(option.resolved);
    const loadedItem =
      getPredictionItemForType(option.resolved, loadedPredictionType) ||
      option.resolved.primary;
    setTrainPredictionType(loadedPredictionType);
    if (loadedItem.best.context_len) {
      setTrainContextLen(Number(loadedItem.best.context_len));
    }
    if (loadedItem.best.horizon_len) {
      setTrainHorizonLen(Number(loadedItem.best.horizon_len));
    }

    const mapped = mapResolvedPredictionToStockData(
      option.resolved,
      language,
      loadedPredictionType,
    );
    if (!mapped?.prediction?.chartData) {
      return;
    }

    const symbol = getItemSymbol(item);
    const itemCompanyName = getItemCompanyName(item);
    const mappedCompanyName = mapped.companyName || "";
    const displayCompanyName =
      itemCompanyName ||
      (mappedCompanyName && mappedCompanyName !== mapped.symbol
        ? mappedCompanyName
        : symbol);
    setSelectedStock({
      ...mapped,
      companyName: displayCompanyName,
      stockType: (item.stock_type || mapped.stockType || 1) as 1 | 2,
    });
    setNextChunkChartData(null);
  };

  const handleChartParameterChange = (
    patch: Partial<
      Pick<
        ChartPredictionOption,
        "modelGroup" | "horizonLen" | "contextLen" | "mtfVersion"
      >
    >,
  ) => {
    if (!selectedWatchlistItem || isTrainingBest || isChartLoading) {
      return;
    }
    const option = pickClosestChartOption(
      chartPredictionOptions,
      activeChartPredictionOption,
      patch,
    );
    if (!option || option.id === activeChartPredictionOptionId) {
      return;
    }
    setChartError(null);
    applyChartPredictionOption(option, selectedWatchlistItem);
  };

  const loadChartPrediction = async (
    item: WatchlistItem,
    requestSeq: number,
    preference?: TrainResultPreference | null,
  ) => {
    const symbol = getItemSymbol(item);
    if (!symbol) {
      return;
    }
    const normalizedSymbol = normalizePredictionSymbol(symbol);

    // Set basic info first
    const currentPrice =
      latestQuotes[symbol]?.latest_price ?? item.current_price?.price ?? 0;
    const changePercent =
      latestQuotes[symbol]?.change_percent ??
      item.current_price?.change_percent ??
      0;

    setSelectedStock({
      symbol: symbol,
      companyName: getItemCompanyName(item),
      currentPrice: currentPrice,
      changePercent: changePercent,
      stockType: (item.stock_type || 1) as 1 | 2,
    } as StockData);
    setSelectedResolvedPrediction(null);

    try {
      const res = await getAccessiblePredictions(undefined, symbol);
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }
      if (res && res.items) {
        const options = buildChartPredictionOptions(
          flattenPublicPredictionItems(res.items || []),
          normalizedSymbol,
          language,
        );
        setChartPredictionOptions(options);
        const found =
          options.find((option) =>
            matchesTrainResultPreference(option.resolved, preference),
          ) || options[0];
        if (found) {
          if (requestSeq !== chartRequestSeqRef.current) {
            return;
          }
          applyChartPredictionOption(found, item, preference?.predictionType);
        }
      }
    } catch (e: any) {
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }
      const message = e?.message || "Failed to fetch prediction for chart";
      if (
        onAuthError &&
        (message.includes("Authorization header required") ||
          message.includes("401") ||
          message.includes("Unauthorized"))
      ) {
        onAuthError();
        return;
      }
      setChartError(message);
      console.error("Failed to fetch prediction for chart", e);
    } finally {
      if (requestSeq === chartRequestSeqRef.current) {
        setIsChartLoading(false);
      }
    }
  };

  const handleShowChart = async (item: WatchlistItem) => {
    if (isWatchlistItemOverLimit(item)) {
      setError(
        language === "zh"
          ? "超出当前会员等级上限。删除其他关注或升级会员后可继续预测。"
          : "Over current membership limit. Delete other items or upgrade to enable predictions.",
      );
      return;
    }
    const requestSeq = chartRequestSeqRef.current + 1;
    chartRequestSeqRef.current = requestSeq;
    const policy = getMTFTrainPolicy(membershipLevel);

    setChartOpen(true);
    setIsChartLoading(true);
    setIsTrainingBest(false);
    setIsPredictingNextChunk(false);
    setChartError(null);
    setShowTrainPanel(false);
    setTrainingJobId(null);
    setTrainingProgress(null);
    setNextChunkChartData(null);
    setChartModalMode("price");
    setSelectedResolvedPrediction(null);
    setChartPredictionOptions([]);
    setActiveChartPredictionOptionId(null);
    setSelectedWatchlistItem(item);
    setTrainPredictionType(policy.defaultPredictionType);
    setTrainContextLen(policy.defaultContextLen);
    setTrainHorizonLen(policy.defaultHorizonLen);

    await loadChartPrediction(item, requestSeq);
  };

  const reloadSelectedStockChart = async (
    requestSeq: number,
    preference?: TrainResultPreference | null,
  ) => {
    if (!selectedWatchlistItem) {
      return;
    }
    setIsChartLoading(true);
    await loadChartPrediction(selectedWatchlistItem, requestSeq, preference);
  };

  const resolveTrainResultPreference = (
    accepted: MTFPredictAcceptedResponse,
    status: MTFJobStatusResponse | null,
    fallback: TrainResultPreference,
  ): TrainResultPreference => ({
    ...fallback,
    covariateSignature:
      status?.covariate_signature ||
      accepted.covariate_signature ||
      fallback.covariateSignature,
  });

  const pollBestTrainingJob = async (
    accepted: MTFPredictAcceptedResponse,
    requestSeq: number,
    fallbackPreference: TrainResultPreference,
  ) => {
    for (
      let attempt = 0;
      attempt < BEST_TRAIN_MAX_POLL_ATTEMPTS;
      attempt += 1
    ) {
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }

      const nextStatus = await mtfAPI.getJobStatus(accepted.job_id);
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }

      if (nextStatus.status === "succeeded") {
        const resolvedPreference = resolveTrainResultPreference(
          accepted,
          nextStatus,
          fallbackPreference,
        );
        trainResultPreferenceRef.current = resolvedPreference;
        await reloadSelectedStockChart(requestSeq, resolvedPreference);
        return;
      }

      if (nextStatus.status === "failed") {
        throw new Error(
          nextStatus.error ||
            nextStatus.result?.error ||
            nextStatus.result?.message ||
            "MTF model training failed",
        );
      }

      await sleep(BEST_TRAIN_POLL_INTERVAL_MS);
    }

    throw new Error("MTF model training timed out");
  };

  const getCurrentBestPredictionItem = (): PublicPredictionItem | null => {
    const item = getPredictionItemForType(
      selectedResolvedPrediction,
      trainPredictionType,
    );
    if (!item) {
      return null;
    }
    if (Number(item.best.context_len || 0) !== trainContextLen) {
      return null;
    }
    if (Number(item.best.horizon_len || 0) !== trainHorizonLen) {
      return null;
    }
    return item;
  };

  const openNextChunkPredictionModal = () => {
    if (!selectedWatchlistItem || !selectedStock) {
      return;
    }

    const bestItem = getCurrentBestPredictionItem();
    if (!bestItem) {
      setChartError(
        language === "zh"
          ? "暂无可用于回测的预测结果，请先执行 MTF 预测推理。"
          : "No prediction result is available for backtesting. Run MTF prediction first.",
      );
      return;
    }

    const stockCode = getTrainingStockCode(selectedWatchlistItem);
    if (!stockCode) {
      setChartError(
        language === "zh" ? "股票代码无效，暂时无法预测" : "Invalid stock code",
      );
      return;
    }

    setChartError(null);
    setNextChunkChartData(null);
    setNextChunkBestItem(bestItem);
    setSinglePredictionMode("next_chunk");
    setChartOpen(false);
    setSinglePredictionItem(selectedWatchlistItem);
  };

  const handleNextChunkPredictionComplete = (
    directResult: DirectPredictionResult,
    request: MTFPredictOnceRequest,
  ) => {
    const bestItem = nextChunkBestItem || getCurrentBestPredictionItem();
    if (!bestItem) {
      throw new Error(
        language === "zh"
          ? "暂无可用于回测的预测结果，请先执行 MTF 预测推理。"
          : "No prediction result is available for backtesting. Run MTF prediction first.",
      );
    }
    const nextChartData = buildNextChunkChartData(
      bestItem,
      directResult,
      (request.prediction_type || trainPredictionType) as TrainPredictionType,
    );
    if (!nextChartData) {
      throw new Error(
        language === "zh"
          ? "MTF 预测完成，但无法生成下一段图表"
          : "MTF prediction finished but chart data is unavailable",
      );
    }
    setNextChunkChartData(nextChartData);
  };

  const handleTrainBest = async () => {
    if (!selectedWatchlistItem || !selectedStock) {
      return;
    }

    const requestSeq = chartRequestSeqRef.current;
    const stockCode = getTrainingStockCode(selectedWatchlistItem);
    if (!stockCode) {
      setChartError("股票代码无效，暂时无法训练");
      return;
    }

    setIsTrainingBest(true);
    setChartError(null);
    setTrainingJobId(null);
    setTrainingProgress(null);
    setTrainingProgressNow(Date.now());
    setNextChunkChartData(null);

    const request: MTFPredictBestRequest = {
      stock_code: stockCode,
      stock_type:
        (selectedWatchlistItem.stock_type || 1) === 2 ? "etf" : "stock",
      prediction_type: trainPredictionType,
      years: DEFAULT_MTF_YEARS,
      horizon_len: trainHorizonLen,
      context_len: trainContextLen,
    };
    const resultPreference: TrainResultPreference = {
      predictionType: trainPredictionType,
      contextLen: trainContextLen,
      horizonLen: trainHorizonLen,
      mtfVersion: DEFAULT_MTF_VERSION,
    };

    try {
      const accepted = await mtfAPI.predictBest(request);
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }
      if (!accepted.job_id) {
        throw new Error("训练任务已提交，但未返回任务 ID");
      }
      setTrainingJobId(accepted.job_id);
      const estimatedSeconds = normalizeEstimatedSeconds(
        accepted.estimated_inference_time_sec,
      );
      if (estimatedSeconds) {
        setTrainingProgress({
          estimatedSeconds,
          startedAt: Date.now(),
        });
      }
      trainResultPreferenceRef.current = resolveTrainResultPreference(
        accepted,
        null,
        resultPreference,
      );
      await pollBestTrainingJob(accepted, requestSeq, resultPreference);
      if (requestSeq === chartRequestSeqRef.current) {
        setShowTrainPanel(false);
      }
    } catch (err: any) {
      if (requestSeq !== chartRequestSeqRef.current) {
        return;
      }
      const message = err?.message || normalizeMTFJobError(err);
      if (
        onAuthError &&
        (message.includes("Authorization header required") ||
          message.includes("401") ||
          message.includes("Unauthorized"))
      ) {
        onAuthError();
        return;
      }
      setChartError(message);
    } finally {
      if (requestSeq === chartRequestSeqRef.current) {
        setIsTrainingBest(false);
        setTrainingProgress(null);
      }
    }
  };

  const handleCloseChart = () => {
    chartRequestSeqRef.current += 1;
    setChartOpen(false);
    setIsChartLoading(false);
    setIsTrainingBest(false);
    setIsPredictingNextChunk(false);
    setTrainingJobId(null);
    setTrainingProgress(null);
    setChartError(null);
    setShowTrainPanel(false);
    setNextChunkChartData(null);
    setChartModalMode("price");
    setSelectedResolvedPrediction(null);
    setChartPredictionOptions([]);
    setActiveChartPredictionOptionId(null);
    trainResultPreferenceRef.current = null;
    setSelectedWatchlistItem(null);
    setSelectedStock(null);
  };

  const filteredItems = watchlistItems.filter((item) => {
    const symbol = getItemSymbol(item);
    const companyName = getItemCompanyName(item);
    const matchesSearch =
      symbol.toLowerCase().includes(searchTerm.toLowerCase()) ||
      companyName.toLowerCase().includes(searchTerm.toLowerCase());
    // Default to 1 (stock) if stock_type is undefined
    const itemType = item.stock_type || 1;
    return !!symbol && matchesSearch && itemType === activeTab;
  });

  const chartDisplayStock =
    selectedStock && selectedStock.prediction
      ? {
          ...selectedStock,
          prediction: {
            ...selectedStock.prediction,
            chartData: nextChunkChartData
              ? nextChunkChartData
              : selectedStock.prediction.chartData,
          },
        }
      : null;

  const shouldShowTrainPanel =
    !!selectedStock && (showTrainPanel || !selectedStock.prediction);
  const isChartChangeMode = chartModalMode === "change";
  const chartModeLabel = isChartChangeMode
    ? language === "zh"
      ? "价格"
      : "Price"
    : language === "zh"
      ? "涨跌幅"
      : "Δ%";
  const chartModeTitle = isChartChangeMode
    ? language === "zh"
      ? "切换到价格走势"
      : "Show price trend"
    : language === "zh"
      ? "切换到涨跌幅对比"
      : "Show change comparison";
  const trainingElapsedSeconds = trainingProgress
    ? Math.max(0, (trainingProgressNow - trainingProgress.startedAt) / 1000)
    : 0;
  const trainingProgressPercent = trainingProgress
    ? Math.min(
        98,
        Math.max(
          0,
          (trainingElapsedSeconds / trainingProgress.estimatedSeconds) * 100,
        ),
      )
    : 0;
  const trainingRemainingPercent = trainingProgress
    ? Math.max(2, Math.min(100, Math.ceil(100 - trainingProgressPercent)))
    : null;
  const isProTraining = trainPredictionType === "mtf-pro";

  const handleCloseTrainPanel = () => {
    if (!selectedStock?.prediction) {
      handleCloseChart();
      return;
    }
    setShowTrainPanel(false);
  };

  const renderTrainPanel = () => {
    if (!selectedStock || !shouldShowTrainPanel) {
      return null;
    }

    return (
      <div
        className="absolute inset-0 z-20 flex items-center justify-center bg-black/55 px-4 py-6 backdrop-blur-sm"
        onClick={handleCloseTrainPanel}
      >
        <div
          className="flex w-full max-w-2xl flex-col gap-4 rounded-[28px] border border-white/10 bg-[#151515] p-5 shadow-[0_28px_90px_rgba(0,0,0,0.45)] md:p-7"
          onClick={(event) => event.stopPropagation()}
        >
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="text-lg font-bold text-white">
                {language === "zh" ? "MTF 模型训练" : "MTF Model Train"}
              </p>
            </div>
            <button
              type="button"
              onClick={() => void handleTrainBest()}
              disabled={isTrainingBest}
              className="flex h-11 shrink-0 items-center justify-center rounded-2xl bg-primary px-4 text-sm font-bold text-black transition-colors hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {isTrainingBest
                ? language === "zh"
                  ? `训练中 ${selectedStock.symbol}`
                  : `Training ${selectedStock.symbol}`
                : language === "zh"
                  ? `训练 ${selectedStock.symbol}`
                  : `Train ${selectedStock.symbol}`}
            </button>
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            <CustomTrainSelect
              label={language === "zh" ? "模型模式" : "Model Mode"}
              value={trainPredictionType}
              options={trainPredictionTypeOptions}
              disabled={
                isTrainingBest || trainPredictionTypeOptions.length <= 1
              }
              onChange={(value) => {
                setTrainPredictionType(value);
                setNextChunkChartData(null);
              }}
            />

            <CustomTrainSelect
              label={language === "zh" ? "预测深度" : "Depth"}
              value={trainContextLen}
              options={trainContextOptions}
              disabled={isTrainingBest || trainContextOptions.length <= 1}
              onChange={(value) => {
                setTrainContextLen(value);
                setNextChunkChartData(null);
              }}
            />

            <CustomTrainSelect
              label={language === "zh" ? "预测周期" : "Period"}
              value={trainHorizonLen}
              options={trainHorizonOptions}
              disabled={isTrainingBest || trainHorizonOptions.length <= 1}
              onChange={(value) => {
                setTrainHorizonLen(value);
                setNextChunkChartData(null);
              }}
            />
          </div>

          {isTrainingBest && (
            <div className="flex flex-col gap-2 rounded-2xl border border-white/10 bg-white/[0.04] px-4 py-3">
              <div className="flex items-center justify-between gap-3 text-xs font-medium text-white/70">
                <span className="truncate">
                  {trainingProgress
                    ? language === "zh"
                      ? "预计剩余"
                      : "Estimated remaining"
                    : language === "zh"
                      ? "已提交训练任务"
                      : "Training request submitted"}
                </span>
                {trainingProgress && (
                  <span className="shrink-0 tabular-nums text-white/85">
                    {trainingRemainingPercent}%
                  </span>
                )}
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-white/10">
                {trainingProgress ? (
                  <div
                    className={`h-full rounded-full transition-[width] duration-500 ease-out ${isProTraining ? "" : "bg-primary"}`}
                    style={{
                      width: `${trainingProgressPercent}%`,
                      ...(isProTraining
                        ? { background: PRO_PROGRESS_GRADIENT }
                        : {}),
                    }}
                  />
                ) : (
                  <div
                    className={`h-full w-0 rounded-full ${isProTraining ? "" : "bg-primary/70"}`}
                    style={
                      isProTraining
                        ? { background: PRO_PROGRESS_GRADIENT }
                        : undefined
                    }
                  />
                )}
              </div>
            </div>
          )}

          {chartError && (
            <div className="rounded-2xl border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
              {chartError}
            </div>
          )}
        </div>
      </div>
    );
  };

  const renderChartHeaderActions = (className = "", showLabel = false) => (
    <div className={`space-y-2 ${className}`.trim()}>
      {showLabel && (
        <div className="text-xs font-semibold text-white/50">
          {language === "zh" ? "操作" : "Actions"}
        </div>
      )}
      <div className="flex flex-nowrap items-center justify-end gap-2">
        {chartDisplayStock?.prediction?.chartData && (
          <button
            type="button"
            aria-label={chartModeTitle}
            title={chartModeTitle}
            onClick={() =>
              setChartModalMode((prev) =>
                prev === "change" ? "price" : "change",
              )
            }
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.06] px-3 text-xs font-semibold text-white/72 shadow-sm transition-colors hover:bg-white/10 hover:text-white"
          >
            <span className="material-symbols-outlined text-[16px] leading-none">
              {isChartChangeMode ? "show_chart" : "percent"}
            </span>
            <span>{chartModeLabel}</span>
          </button>
        )}
        <button
          type="button"
          onClick={() => setShowTrainPanel(true)}
          className="inline-flex h-9 items-center rounded-lg border border-white/10 bg-white/[0.06] px-3 text-xs font-bold text-white/80 transition-colors hover:bg-white/10 hover:text-white"
        >
          {language === "zh" ? "继续训练" : "Train"}
        </button>
        <button
          onClick={openNextChunkPredictionModal}
          disabled={
            !selectedStock?.prediction ||
            isPredictingNextChunk ||
            isTrainingBest
          }
          className={`inline-flex h-9 items-center rounded-lg border px-3 text-xs font-bold transition-colors ${
            isPredictingNextChunk || isTrainingBest
              ? "cursor-not-allowed border-white/10 bg-white/[0.03] text-white/35"
              : "border-amber-300/25 bg-amber-300/10 text-amber-100 hover:bg-amber-300/16 hover:text-amber-50"
          } ${!selectedStock?.prediction ? "cursor-not-allowed opacity-40 hover:bg-transparent" : ""}`}
        >
          {isPredictingNextChunk
            ? language === "zh"
              ? "预测中"
              : "Running"
            : language === "zh"
              ? "MTF 预测"
              : "Predict"}
        </button>
        <button
          type="button"
          aria-label={language === "zh" ? "关闭" : "Close"}
          title={language === "zh" ? "关闭" : "Close"}
          onClick={handleCloseChart}
          className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-white/10 bg-white/[0.06] text-white/60 transition-colors hover:bg-white/10 hover:text-white"
        >
          <span className="material-symbols-outlined text-[18px]">close</span>
        </button>
      </div>
    </div>
  );

  const renderChartParameterSelector = (
    className = "",
    actionSlot?: React.ReactNode,
  ) => {
    if (chartPredictionOptions.length <= 1) {
      return null;
    }

    const shouldShowVersionSelector = chartVersionParameterOptions.length > 1;
    const shouldShowHorizonSelector = chartHorizonParameterOptions.length > 1;
    const shouldShowContextSelector = chartContextParameterOptions.length > 1;

    return (
      <div className={className.trim()}>
        <div className="flex min-w-0 flex-col gap-3 md:flex-row md:items-start md:justify-between md:gap-4">
          <div className="min-w-0 flex-1">
            <p className="text-lg font-bold text-white">
              {selectedStock?.companyName || selectedStock?.symbol}
            </p>
            <p className="text-sm text-white/55">{selectedStock?.symbol}</p>
            {(shouldShowVersionSelector ||
              shouldShowHorizonSelector ||
              shouldShowContextSelector) && (
              <div className="mt-3 flex flex-wrap gap-3">
                {shouldShowVersionSelector && (
                  <div className="min-w-fit">
                    {renderChartParameterGroup(
                      language === "zh" ? "模型版本" : "Version",
                      chartVersionParameterOptions.map((option) =>
                        renderChartParameterButton(
                          option.mtfVersion,
                          `v${option.mtfVersion}`,
                          activeChartPredictionOption?.mtfVersion ===
                            option.mtfVersion,
                          () =>
                            handleChartParameterChange({
                              mtfVersion: option.mtfVersion,
                            }),
                          isTrainingBest || isChartLoading,
                        ),
                      ),
                    )}
                  </div>
                )}
                {shouldShowHorizonSelector && (
                  <div className="min-w-fit">
                    {renderChartParameterGroup(
                      language === "zh" ? "预测周期" : "Period",
                      chartHorizonParameterOptions.map((option) =>
                        renderChartParameterButton(
                          String(option.horizonLen),
                          `${option.horizonLen}d`,
                          activeChartPredictionOption?.horizonLen ===
                            option.horizonLen,
                          () =>
                            handleChartParameterChange({
                              horizonLen: option.horizonLen,
                            }),
                          isTrainingBest || isChartLoading,
                        ),
                      ),
                    )}
                  </div>
                )}
                {shouldShowContextSelector && (
                  <div className="min-w-fit">
                    {renderChartParameterGroup(
                      language === "zh" ? "预测深度" : "Depth",
                      chartContextParameterOptions.map((option) =>
                        renderChartParameterButton(
                          String(option.contextLen),
                          String(option.contextLen),
                          activeChartPredictionOption?.contextLen ===
                            option.contextLen,
                          () =>
                            handleChartParameterChange({
                              contextLen: option.contextLen,
                            }),
                          isTrainingBest || isChartLoading,
                        ),
                      ),
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
          {actionSlot && <div className="shrink-0">{actionSlot}</div>}
        </div>
      </div>
    );
  };

  return (
    <div className="flex flex-col w-full">
      <div className="flex flex-wrap justify-between gap-3 p-4 mb-4">
        <div className="flex min-w-72 flex-col gap-2">
          <p className="text-white text-4xl font-black leading-tight tracking-[-0.033em]">
            {t("watchlist.title")}
          </p>
          <p className="text-white/60 text-base font-normal leading-normal">
            {t("watchlist.subtitle")}
          </p>
        </div>
        <button
          type="button"
          onClick={() => void loadWatchlist()}
          disabled={isLoading}
          className="flex h-10 items-center justify-center gap-2 rounded-lg border border-white/10 bg-white/5 px-4 text-sm font-bold text-white transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-60"
        >
          <span
            className={`material-symbols-outlined text-[20px] ${isLoading ? "animate-spin" : ""}`}
          >
            refresh
          </span>
          <span>
            {t("uzi.refresh", language === "zh" ? "刷新" : "Refresh")}
          </span>
        </button>
      </div>

      {error && (
        <div
          onClick={onAuthError}
          className={`mx-4 mb-3 p-2 bg-red-500/10 border border-red-500/20 rounded-lg ${onAuthError ? "cursor-pointer hover:bg-red-500/20 transition-colors" : ""}`}
        >
          <p className="text-red-400 text-sm">
            {(() => {
              if (error.includes("symbol not found")) {
                return t("addStock.errorNotFound");
              }
              if (
                error.includes("User not authenticated") ||
                error.includes("Unauthorized")
              ) {
                return t("addStock.errorAuth");
              }
              if (error.includes("watchlist limit exceeded")) {
                return t("addStock.errorLimitExceeded");
              }
              return error;
            })()}
          </p>
        </div>
      )}

      <div className="mb-4 px-4">
        <div className="flex items-center gap-2 lg:justify-between">
          <div className="flex shrink-0 space-x-1 rounded-lg bg-white/5 p-1">
            <button
              className={`h-9 rounded-md px-3 text-xs font-bold transition-all sm:h-10 sm:px-6 sm:text-sm ${activeTab === 1 ? "bg-primary text-black shadow-sm" : "text-white/60 hover:text-white hover:bg-white/5"}`}
              onClick={() => setActiveTab(1)}
            >
              {t("watchlist.tabStock")}
            </button>
            <button
              className={`h-9 rounded-md px-3 text-xs font-bold transition-all sm:h-10 sm:px-6 sm:text-sm ${activeTab === 2 ? "bg-primary text-black shadow-sm" : "text-white/60 hover:text-white hover:bg-white/5"}`}
              onClick={() => setActiveTab(2)}
            >
              {t("watchlist.tabEtf")}
            </button>
          </div>

          <div className="ml-auto flex min-w-0 flex-1 items-center justify-end gap-2 sm:flex-none">
            <div className="flex h-10 min-w-0 flex-1 items-stretch rounded-lg border border-white/10 bg-white/5 transition-all duration-200 focus-within:border-primary focus-within:ring-2 focus-within:ring-primary/50 sm:h-12 sm:min-w-[280px] sm:flex-none">
              <div className="flex items-center justify-center pl-3 text-white/60 sm:pl-4">
                <span className="material-symbols-outlined text-[18px] sm:text-[24px]">
                  search
                </span>
              </div>
              <input
                className="form-input flex h-full w-full min-w-0 flex-1 resize-none overflow-hidden rounded-lg border-none bg-transparent px-2 pl-1 text-sm font-normal leading-normal text-white placeholder:text-white/40 focus:outline-0 focus:ring-0 sm:px-4 sm:pl-2 sm:text-base"
                placeholder={t("watchlist.searchPlaceholder")}
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
              />
            </div>

            <button
              className="flex h-10 w-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-primary px-0 text-sm font-bold text-black transition-colors hover:bg-opacity-90 disabled:cursor-not-allowed disabled:opacity-50 sm:h-12 sm:w-auto sm:px-5"
              onClick={() => setIsModalOpen(true)}
              disabled={isLoading}
              aria-label={t("watchlist.addStock")}
            >
              <span className="material-symbols-outlined text-[20px] sm:text-[24px]">
                add
              </span>
              <span className="hidden truncate sm:inline">
                {t("watchlist.addStock")}
              </span>
            </button>
          </div>
        </div>
      </div>

      <AddStockModal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        onAdd={handleAddStock}
      />

      <ConfirmModal
        isOpen={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
        onConfirm={handleRemoveStock}
        title={t("modal.deleteTitle")}
        message={t("modal.deleteMessage").replace(
          "{symbol}",
          stockToDelete?.symbol || "",
        )}
        confirmText={t("modal.deleteConfirm")}
        cancelText={t("modal.deleteCancel")}
        isDanger={true}
        isLoading={isLoading}
      />

      {singlePredictionItem && (
        <SinglePredictionModal
          isOpen={true}
          item={singlePredictionItem}
          mode={singlePredictionMode}
          currentPrice={
            latestQuotes[getItemSymbol(singlePredictionItem)]?.latest_price ??
            singlePredictionItem.current_price?.price
          }
          initialPredictionType={trainPredictionType}
          predictionTypeOptions={trainPolicy.predictionTypes}
          initialContextLen={trainContextLen}
          initialHorizonLen={trainHorizonLen}
          contextOptions={trainPolicy.contextLens}
          horizonOptions={trainPolicy.horizonLens}
          mtfVersion={
            singlePredictionMode === "next_chunk"
              ? String(nextChunkBestItem?.best.mtf_version || "").trim() ||
                DEFAULT_MTF_VERSION
              : undefined
          }
          enableCachedLookup
          onClose={closeSinglePredictionModal}
          onAuthError={onAuthError}
          onSubmittingChange={(submitting) => {
            if (singlePredictionMode === "next_chunk") {
              setIsPredictingNextChunk(submitting);
            }
          }}
          onPredictionComplete={(result, request) => {
            if (singlePredictionMode === "next_chunk") {
              handleNextChunkPredictionComplete(result, request);
            }
          }}
        />
      )}

      {/* Chart Modal */}
      {chartOpen && selectedStock && (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/80 backdrop-blur-sm p-0 md:items-center md:p-4">
          <div className="flex h-[78vh] max-h-[92vh] w-full flex-col overflow-hidden rounded-t-[28px] border border-white/10 bg-card-dark md:h-auto md:max-w-6xl md:rounded-xl xl:max-w-[1320px]">
            <div className="border-b border-white/10 p-4 md:p-6">
              <div className="mb-3 flex items-center justify-center md:hidden">
                <div className="h-1.5 w-12 rounded-full bg-white/15" />
              </div>
              <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                {chartPredictionOptions.length > 1 ? (
                  renderChartParameterSelector(
                    "min-w-0 flex-1 rounded-2xl border border-white/10 bg-black/20 p-3",
                    renderChartHeaderActions("", true),
                  )
                ) : (
                  <div className="min-w-0 flex-1">
                    <p className="text-lg font-bold text-white">
                      {selectedStock.companyName || selectedStock.symbol}
                    </p>
                    <p className="text-sm text-white/55">
                      {selectedStock.symbol}
                    </p>
                  </div>
                )}
                {chartPredictionOptions.length <= 1 &&
                  renderChartHeaderActions("shrink-0 pr-1 md:pr-2")}
              </div>
              {chartDisplayStock && (
                <StockPredictionSummary
                  stock={chartDisplayStock}
                  chartMode={chartModalMode}
                  onChartModeChange={setChartModalMode}
                  className="mt-4 rounded-2xl border border-white/10 bg-black/20 p-3"
                  nameColumnClassName="md:min-w-[86px] md:max-w-[86px] md:flex-[0_0_86px]"
                  hideTitle
                  horizonOptions={chartSummaryHorizonOptions}
                  selectedHorizon={activeChartPredictionOption?.horizonLen}
                  onHorizonChange={(value) =>
                    handleChartParameterChange({ horizonLen: value })
                  }
                  contextOptions={chartSummaryContextOptions}
                  selectedContext={activeChartPredictionOption?.contextLen}
                  onContextChange={(value) =>
                    handleChartParameterChange({ contextLen: value })
                  }
                />
              )}
            </div>
            <div className="relative flex min-h-0 flex-1 flex-col overflow-y-auto bg-card-dark p-4 md:min-h-[300px] md:p-6">
              {isChartLoading ? (
                <div className="flex-1 flex items-center justify-center">
                  <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
                </div>
              ) : (
                <>
                  {chartError &&
                    selectedStock.prediction &&
                    !shouldShowTrainPanel && (
                      <div className="mb-4 rounded-2xl border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
                        {chartError}
                      </div>
                    )}

                  {chartDisplayStock && (
                    <StockPredictionCard
                      stock={chartDisplayStock}
                      className="border-white/8 bg-transparent p-0 shadow-none"
                      chartHeightClassName="h-[260px] md:h-[360px] lg:h-[420px]"
                      borderless
                      hideSummary
                      chartMode={chartModalMode}
                      onChartModeChange={setChartModalMode}
                    />
                  )}

                  {renderTrainPanel()}
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {watchlistItems.length > 0 ? (
        <div className="px-4 py-2 @container">
          <div className="space-y-3 md:hidden">
            {filteredItems.map((item) => {
              const symbol = getItemSymbol(item);
              const companyName = getItemCompanyName(item);
              const changePercent =
                (latestQuotes[symbol]?.change_percent ??
                  item.current_price?.change_percent) ||
                0;
              const isPositive = changePercent >= 0;
              const { textClass } = getChangeColors(isPositive, language);
              const currentPrice =
                latestQuotes[symbol]?.latest_price ?? item.current_price?.price;
              const overLimit = isWatchlistItemOverLimit(item);

              return (
                <div
                  key={item.id}
                  className={`rounded-2xl border bg-black/20 p-4 ${overLimit ? "border-amber-300/30 opacity-75" : "border-white/10"}`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="font-mono text-sm font-semibold text-white">
                          {symbol.toLowerCase()}
                        </p>
                        {overLimit && (
                          <span className="rounded-full border border-amber-300/30 bg-amber-300/10 px-2 py-0.5 text-[11px] font-bold text-amber-200">
                            {t("watchlist.overLimitBadge")}
                          </span>
                        )}
                      </div>
                      <p className="mt-1 text-sm text-white/55">
                        {companyName}
                      </p>
                      {overLimit && (
                        <p className="mt-2 text-xs leading-5 text-amber-100/75">
                          {t("watchlist.overLimitHint")}
                        </p>
                      )}
                    </div>
                    <div className="text-right">
                      <p className="text-sm font-semibold text-white">
                        {currentPrice != null ? currentPrice.toFixed(2) : "—"}
                      </p>
                      <p className={`mt-1 text-xs font-medium ${textClass}`}>
                        {currentPrice != null && changePercent != null ? (
                          <>
                            {isPositive ? "+" : ""}
                            {((currentPrice * changePercent) / 100).toFixed(
                              2,
                            )}{" "}
                            ({isPositive ? "+" : ""}
                            {changePercent.toFixed(2)}%)
                          </>
                        ) : (
                          "—"
                        )}
                      </p>
                    </div>
                  </div>

                  <div className="mt-4 grid grid-cols-2 gap-3">
                    <div className="rounded-xl border border-white/8 bg-white/[0.03] p-3">
                      <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">
                        {t("watchlist.latestDate")}
                      </p>
                      <p className="mt-2 text-sm font-medium text-white/80">
                        {latestQuotes[symbol]?.trading_date || "—"}
                      </p>
                    </div>
                    <div className="rounded-xl border border-white/8 bg-white/[0.03] p-3">
                      <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">
                        {t("watchlist.turnoverRate")}
                      </p>
                      <p className="mt-2 text-sm font-medium text-white/80">
                        {latestQuotes[symbol]?.turnover_rate != null
                          ? `${(latestQuotes[symbol]!.turnover_rate! * 100).toFixed(2)}%`
                          : "—"}
                      </p>
                    </div>
                  </div>

                  <div className="mt-4 grid grid-cols-3 gap-2">
                    <button
                      onClick={() => openStandalonePredictionModal(item)}
                      className="flex h-10 items-center justify-center gap-1 rounded-xl border border-white/10 bg-white/5 text-xs font-medium text-white/85 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-white/5"
                      disabled={isLoading || overLimit}
                      title={
                        overLimit
                          ? t("watchlist.overLimitHint")
                          : t("watchlist.singlePredict")
                      }
                    >
                      <span className="material-symbols-outlined text-[18px]">
                        timeline
                      </span>
                      <span>{t("watchlist.singlePredict")}</span>
                    </button>
                    <button
                      onClick={() => handleShowChart(item)}
                      className="flex h-10 items-center justify-center gap-1 rounded-xl border border-primary/20 bg-primary/10 text-xs font-medium text-primary transition-colors hover:bg-primary/15 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-primary/10"
                      disabled={isLoading || overLimit}
                      title={
                        overLimit
                          ? t("watchlist.overLimitHint")
                          : t("watchlist.showChart")
                      }
                    >
                      <span className="material-symbols-outlined text-[18px]">
                        query_stats
                      </span>
                      <span>{t("watchlist.showChart")}</span>
                    </button>
                    <button
                      onClick={() => confirmRemove(item.id)}
                      className="flex h-10 items-center justify-center gap-1 rounded-xl border border-red-500/20 bg-red-500/10 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-red-500/10"
                      disabled={isLoading}
                    >
                      <span className="material-symbols-outlined text-[18px]">
                        delete
                      </span>
                      <span>{t("modal.deleteConfirm")}</span>
                    </button>
                  </div>
                </div>
              );
            })}
          </div>

          <div className="hidden overflow-x-auto overscroll-x-contain rounded-xl border border-[#2D2D2D] bg-black/20 touch-pan-x md:block">
            <table className="min-w-[920px] w-full">
              <thead className="border-b border-b-[#2D2D2D]">
                <tr>
                  <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.ticker")}
                  </th>
                  <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.latestDate")}
                  </th>
                  <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal hidden sm:table-cell">
                    {t("watchlist.lastPrice")}
                  </th>
                  <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.todayChange")}
                  </th>
                  <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.turnoverRate")}
                  </th>
                  <th className="px-4 py-3 text-center text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.singlePredict")}
                  </th>
                  <th className="px-4 py-3 text-center text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.showChart")}
                  </th>
                  <th className="px-4 py-3 text-center text-white/60 text-sm font-medium leading-normal">
                    {t("watchlist.actions")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {filteredItems.map((item) => {
                  const symbol = getItemSymbol(item);
                  const companyName = getItemCompanyName(item);
                  const changePercent =
                    (latestQuotes[symbol]?.change_percent ??
                      item.current_price?.change_percent) ||
                    0;
                  const isPositive = changePercent >= 0;
                  const { textClass } = getChangeColors(isPositive, language);
                  const currentPrice =
                    latestQuotes[symbol]?.latest_price ??
                    item.current_price?.price;
                  const overLimit = isWatchlistItemOverLimit(item);

                  return (
                    <tr
                      key={item.id}
                      className={`border-t border-t-[#2D2D2D] ${overLimit ? "bg-amber-300/[0.03] opacity-75" : ""}`}
                    >
                      <td className="h-[72px] px-4 py-2 text-white text-sm font-normal leading-normal">
                        <span className="font-bold">
                          {symbol.toLowerCase()}
                        </span>
                        {overLimit && (
                          <span className="ml-2 rounded-full border border-amber-300/30 bg-amber-300/10 px-2 py-0.5 text-[11px] font-bold text-amber-200">
                            {t("watchlist.overLimitBadge")}
                          </span>
                        )}
                        <br />
                        <span className="text-xs text-white/60">
                          {companyName}
                        </span>
                        {overLimit && (
                          <p className="mt-1 max-w-[260px] text-xs text-amber-100/70">
                            {t("watchlist.overLimitHint")}
                          </p>
                        )}
                      </td>
                      <td className="h-[72px] px-4 py-2 text-white/60 text-sm">
                        {latestQuotes[symbol]?.trading_date || "—"}
                      </td>
                      <td className="h-[72px] px-4 py-2 text-white/80 text-sm font-normal leading-normal hidden sm:table-cell">
                        {currentPrice != null ? currentPrice.toFixed(2) : "—"}
                      </td>
                      <td
                        className={`h-[72px] px-4 py-2 text-sm font-normal leading-normal ${textClass}`}
                      >
                        {currentPrice != null && changePercent != null ? (
                          <>
                            {isPositive ? "+" : ""}
                            {((currentPrice * changePercent) / 100).toFixed(
                              2,
                            )}{" "}
                            ({isPositive ? "+" : ""}
                            {changePercent.toFixed(2)}%)
                          </>
                        ) : (
                          "—"
                        )}
                      </td>
                      <td className="h-[72px] px-4 py-2 text-white/60 text-sm">
                        {latestQuotes[symbol]?.turnover_rate != null
                          ? `${(latestQuotes[symbol]!.turnover_rate! * 100).toFixed(2)}%`
                          : "—"}
                      </td>
                      <td className="h-[72px] px-4 py-2 text-center">
                        <button
                          onClick={() => openStandalonePredictionModal(item)}
                          className="p-2 rounded-full hover:bg-white/10 text-white/80 transition-colors disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                          disabled={isLoading || overLimit}
                          title={
                            overLimit
                              ? t("watchlist.overLimitHint")
                              : t("watchlist.singlePredict")
                          }
                        >
                          <span
                            className="material-symbols-outlined"
                            style={{ fontSize: "20px" }}
                          >
                            timeline
                          </span>
                        </button>
                      </td>
                      <td className="h-[72px] px-4 py-2 text-center">
                        <button
                          onClick={() => handleShowChart(item)}
                          className="p-2 rounded-full hover:bg-white/10 text-primary transition-colors disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                          disabled={isLoading || overLimit}
                          title={
                            overLimit
                              ? t("watchlist.overLimitHint")
                              : t("watchlist.showChart")
                          }
                        >
                          <span
                            className="material-symbols-outlined"
                            style={{ fontSize: "20px" }}
                          >
                            query_stats
                          </span>
                        </button>
                      </td>
                      <td className="h-[72px] px-4 py-2 text-center">
                        <button
                          onClick={() => confirmRemove(item.id)}
                          className="p-2 rounded-full hover:bg-white/10 text-red-500 transition-colors disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                          disabled={isLoading}
                        >
                          <span
                            className="material-symbols-outlined"
                            style={{ fontSize: "20px" }}
                          >
                            delete
                          </span>
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <div className="flex flex-col px-4 py-16 mt-8">
          <div className="flex flex-col items-center gap-6">
            <div className="text-primary">
              <span
                className="material-symbols-outlined"
                style={{ fontSize: "96px" }}
              >
                playlist_add
              </span>
            </div>
            <div className="flex max-w-[480px] flex-col items-center gap-2">
              <p className="text-white text-lg font-bold leading-tight tracking-[-0.015em] max-w-[480px] text-center">
                {t("watchlist.emptyTitle")}
              </p>
              <p className="text-white/60 text-sm font-normal leading-normal max-w-[480px] text-center">
                {t("watchlist.emptySubtitle")}
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default Watchlist;
