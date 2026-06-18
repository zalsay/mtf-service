import React, { useState, useEffect, useCallback } from 'react';
import { PredictionChartData, StockData } from '../../types';
import StrategyCard from '../dashboard/StrategyCard';
import CreateStrategyModal from '../dashboard/CreateStrategyModal';
import BindStrategyModal from './BindStrategyModal';
import { useLanguage } from '../../contexts/LanguageContext';
import { watchlistAPI, strategyAPI, backtestAPI, authAPI, getAccessiblePredictions } from '../../services/apiService';
import { type PredictionChartMarker, type PredictionChartTheme } from '../common/PredictionChart';
import PredictionChartPanel from '../common/PredictionChartPanel';
import type { PublicPredictionItem } from '../../types';
import { flattenPublicPredictionItems, isMTFProPredictionItem, isMTFProUniqueKey } from '../../utils/predictionUtils';

interface PortfolioProps {
    onAuthError?: () => void;
}

interface BacktestConfig {
    uniqueKey: string;
    predictionType: 'mtf-lite' | 'mtf-pro';
    horizonLen: number;
    contextLen: number;
    mtfVersion: string;
    updatedAt?: string;
    chunkCount: number;
}

interface BacktestConfigModalProps {
    isOpen: boolean;
    item: any | null;
    strategy: any | null;
    isSubmitting: boolean;
    onClose: () => void;
    onSubmit: (uniqueKey: string, strategy: any, symbol: string) => void;
    onAuthError?: () => void;
}

interface BacktestChartModalProps {
    isOpen: boolean;
    result: any | null;
    title: string;
    configs: BacktestConfig[];
    selectedUniqueKey: string;
    isLoading?: boolean;
    onSelectConfig: (config: BacktestConfig) => void;
    onClose: () => void;
}

interface BacktestNotice {
    type: 'success' | 'error' | 'info';
    title: string;
    message: string;
    totalReturn?: number;
    tradeCount?: number;
    canOpenChart?: boolean;
}

interface BacktestMetric {
    icon: string;
    label: string;
    value: string;
    tone?: 'positive' | 'negative' | 'neutral';
}

interface BacktestTradeMarker extends PredictionChartMarker {
    positionAfter?: number;
    size?: number;
    predictedPctChange?: number;
}

interface BacktestTradeDecisionContext {
    predictedPctChange?: number;
    buyThresholdPct?: number;
    sellThresholdPct?: number;
}

type BacktestAssetViewMode = 'chart' | 'table';
type BacktestChartLayoutMode = 'overview' | 'detail';

interface CompactStrategyCardProps {
    strategy: any;
    language: string;
}

const A_STOCK_SHARE_LOT = 100;
const SHARE_LOT_EPSILON = 0.000001;

const escapeExcelCell = (value: unknown): string => String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');

const formatExcelNumber = (value: unknown, digits = 4): string => {
    const number = Number(value);
    return Number.isFinite(number) ? number.toFixed(digits) : '';
};

const formatBacktestNumber = (value: unknown, digits = 2): string => {
    const number = Number(value);
    return Number.isFinite(number) ? number.toFixed(digits) : '—';
};

const normalizeBacktestShareLot = (value: unknown): number | undefined => {
    const number = Number(value);
    if (!Number.isFinite(number)) return undefined;
    if (number <= SHARE_LOT_EPSILON) return 0;
    if (number < A_STOCK_SHARE_LOT) return A_STOCK_SHARE_LOT;
    return Math.floor((number + SHARE_LOT_EPSILON) / A_STOCK_SHARE_LOT) * A_STOCK_SHARE_LOT;
};

const formatBacktestShares = (value: unknown): string => {
    const shares = normalizeBacktestShareLot(value);
    return shares === undefined ? '—' : shares.toFixed(0);
};

const formatExcelShares = (value: unknown): string => {
    const shares = normalizeBacktestShareLot(value);
    return shares === undefined ? '' : shares.toFixed(0);
};

const formatExcelPercent = (value: unknown): string => {
    const number = Number(value);
    return Number.isFinite(number) ? `${number.toFixed(2)}%` : '';
};

const formatCompactNumber = (value: string, multiplier = 1): string => {
    const number = Number(value);
    if (!Number.isFinite(number)) return value.trim();
    return (number * multiplier).toFixed(2).replace(/\.?0+$/, '');
};

const CompactStrategyCard: React.FC<CompactStrategyCardProps> = ({ strategy, language }) => {
    const buy = Number(strategy.buy_threshold_pct);
    const sell = Number(strategy.sell_threshold_pct);
    const minPosition = Number(strategy.min_position_pct);
    const maxPosition = Number(strategy.max_position_pct);

    return (
        <div className="min-w-0 rounded-lg border border-white/10 bg-white/[0.045] p-2.5">
            <div className="mb-2 flex min-w-0 items-center justify-between gap-1.5">
                <p className="min-w-0 truncate text-[12px] font-bold leading-5 text-white sm:text-sm">
                    {strategy.name || 'Strategy'}
                </p>
                <span className="shrink-0 rounded bg-blue-500/15 px-1.5 py-0.5 text-[9px] font-bold text-blue-300">
                    {language === 'zh' ? '官方' : 'Official'}
                </span>
            </div>
            <div className="grid grid-cols-2 gap-1.5 text-[10px] leading-4 text-white/45 sm:text-xs">
                <div className="rounded-md bg-black/12 px-2 py-1">
                    <span>{language === 'zh' ? '买/卖' : 'Buy/Sell'}</span>
                    <p className="mt-0.5 font-mono font-bold text-white/85">
                        {Number.isFinite(buy) ? buy : '—'}|{Number.isFinite(sell) ? sell : '—'}%
                    </p>
                </div>
                <div className="rounded-md bg-black/12 px-2 py-1">
                    <span>{language === 'zh' ? '仓位' : 'Range'}</span>
                    <p className="mt-0.5 font-mono font-bold text-white/85">
                        {Number.isFinite(minPosition) ? Math.round(minPosition * 100) : '—'}-{Number.isFinite(maxPosition) ? Math.round(maxPosition * 100) : '—'}%
                    </p>
                </div>
            </div>
        </div>
    );
};

const extractTradeDecisionValue = (reason: unknown, context: BacktestTradeDecisionContext = {}): string => {
    const rawReason = String(reason ?? '').trim();
    if (!rawReason) return '';

    const takeProfitMatch = rawReason.match(/^take_profit\s*>=\s*(-?\d+(?:\.\d+)?)$/i);
    if (takeProfitMatch) return `${Number(takeProfitMatch[1]).toFixed(2)}%`;

    const predictionMatch = rawReason.match(/^pred_pct\s*(>=|<=)\s*(-?\d+(?:\.\d+)?)$/i);
    if (predictionMatch) return `${Number(predictionMatch[2]).toFixed(2)}%`;

    const rebalanceMatch = rawReason.match(/^rebalance_(?:up|down)\s*->\s*(-?\d+(?:\.\d+)?)$/i);
    if (rebalanceMatch) {
        const predictedPctChange = Number(context.predictedPctChange);
        const buyThresholdPct = Number(context.buyThresholdPct);
        const sellThresholdPct = Number(context.sellThresholdPct);
        if (Number.isFinite(predictedPctChange) && Number.isFinite(buyThresholdPct) && predictedPctChange >= buyThresholdPct) {
            return `${buyThresholdPct.toFixed(2)}%`;
        }
        if (Number.isFinite(predictedPctChange) && Number.isFinite(sellThresholdPct) && predictedPctChange <= sellThresholdPct) {
            return `${sellThresholdPct.toFixed(2)}%`;
        }
        return `${(Number(rebalanceMatch[1]) * 100).toFixed(2)}%`;
    }

    return '';
};

const formatTradeDecisionValueList = (
    markers: BacktestTradeMarker[],
    context: Omit<BacktestTradeDecisionContext, 'predictedPctChange'> = {},
): string => (
    markers
        .map(marker => extractTradeDecisionValue(marker.reason, {
            ...context,
            predictedPctChange: marker.predictedPctChange,
        }))
        .join(' / ')
);

const formatPredictedPctChangeList = (markers: BacktestTradeMarker[]): string => (
    markers
        .map(marker => formatExcelPercent(marker.predictedPctChange))
        .join(' / ')
);

const formatTradeReason = (reason: unknown, language: string): string => {
    const rawReason = String(reason ?? '').trim();
    if (!rawReason) return '';

    const isZh = language === 'zh';
    const takeProfitMatch = rawReason.match(/^take_profit\s*>=\s*(-?\d+(?:\.\d+)?)$/i);
    if (takeProfitMatch) {
        const threshold = formatCompactNumber(takeProfitMatch[1]);
        return isZh
            ? `止盈触发（收益 ≥ ${threshold}%）`
            : `Take profit triggered (return ≥ ${threshold}%)`;
    }

    const rebalanceMatch = rawReason.match(/^rebalance_(up|down)\s*->\s*(-?\d+(?:\.\d+)?)$/i);
    if (rebalanceMatch) {
        const targetPosition = formatCompactNumber(rebalanceMatch[2], 100);
        if (rebalanceMatch[1].toLowerCase() === 'up') {
            return isZh
                ? `上调仓位（目标仓位 ${targetPosition}%）`
                : `Rebalance up (target position ${targetPosition}%)`;
        }
        return isZh
            ? `下调仓位（目标仓位 ${targetPosition}%）`
            : `Rebalance down (target position ${targetPosition}%)`;
    }

    const predictionMatch = rawReason.match(/^pred_pct\s*(>=|<=)\s*(-?\d+(?:\.\d+)?)$/i);
    if (predictionMatch) {
        const threshold = formatCompactNumber(predictionMatch[2]);
        if (predictionMatch[1] === '>=') {
            return isZh
                ? `预测涨幅达标（≥ ${threshold}%）`
                : `Predicted return met buy threshold (≥ ${threshold}%)`;
        }
        return isZh
            ? `预测跌幅触发卖出（≤ ${threshold}%）`
            : `Predicted return hit sell threshold (≤ ${threshold}%)`;
    }

    return rawReason;
};

const formatTradeReasonList = (markers: PredictionChartMarker[], language: string): string => (
    markers
        .map(marker => formatTradeReason(marker.reason, language))
        .filter(Boolean)
        .join(' / ')
);

const safeFileName = (value: string): string => (
    String(value || 'backtest')
        .trim()
        .replace(/[\\/:*?"<>|]+/g, '_')
        .replace(/\s+/g, '_')
        .slice(0, 80) || 'backtest'
);

const normalizeSymbol = (value: string): string => {
    const trimmed = String(value || '').trim().toLowerCase();
    return trimmed.replace(/\D/g, '') || trimmed;
};

const getItemSymbol = (item: any) => item?.stock?.symbol || '';
const getItemCompanyName = (item: any) => item?.stock?.company_name || getItemSymbol(item);
const getBacktestUniqueKey = (item: any) => item?.unique_key || '';
const BACKTEST_MODEL_OPTIONS: Array<'mtf-lite' | 'mtf-pro'> = ['mtf-lite', 'mtf-pro'];
const BACKTEST_HORIZON_OPTIONS = [7, 14, 28];
const BACKTEST_CONTEXT_OPTIONS = [512, 1024, 2048];
const liteMetricHeaderStyle = {
    backgroundColor: 'rgba(226, 232, 240, 0.14)',
};
const proMetricHeaderStyle = {
    backgroundImage: 'linear-gradient(90deg, rgba(255,241,184,0.75) 0%, rgba(252,211,77,0.75) 34%, rgba(245,158,11,0.75) 68%, rgba(249,115,22,0.75) 100%)',
};

const isMTFProPrediction = (item: PublicPredictionItem) => {
    return isMTFProPredictionItem(item);
};

const buildBacktestConfigs = (items: PublicPredictionItem[], symbol: string): BacktestConfig[] => {
    const normalizedSymbol = normalizeSymbol(symbol);
    return (items || [])
        .filter(item => normalizeSymbol(item.best.symbol) === normalizedSymbol)
        .map(item => ({
            uniqueKey: item.best.unique_key,
            predictionType: isMTFProPrediction(item) ? 'mtf-pro' : 'mtf-lite',
            horizonLen: Number(item.best.horizon_len || 0),
            contextLen: Number(item.best.context_len || 0),
            mtfVersion: String(item.best.mtf_version || '2.5'),
            updatedAt: item.best.updated_at,
            chunkCount: item.chunks?.length || 0,
        }))
        .filter(config => config.uniqueKey && config.horizonLen > 0 && config.contextLen > 0 && config.chunkCount > 0)
        .sort((a, b) => {
            const timeDiff = new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime();
            if (timeDiff) return timeDiff;
            if (a.predictionType !== b.predictionType) return a.predictionType === 'mtf-pro' ? -1 : 1;
            return b.contextLen - a.contextLen || b.horizonLen - a.horizonLen;
        });
};

const formatContextLen = (value: number) => value >= 1024 ? `${Math.round(value / 1024)}K` : String(value);

const BacktestConfigModal: React.FC<BacktestConfigModalProps> = ({
    isOpen,
    item,
    strategy,
    isSubmitting,
    onClose,
    onSubmit,
    onAuthError,
}) => {
    const { language } = useLanguage();
    const [configs, setConfigs] = useState<BacktestConfig[]>([]);
    const [selectedType, setSelectedType] = useState<'mtf-lite' | 'mtf-pro'>('mtf-lite');
    const [selectedHorizon, setSelectedHorizon] = useState<number>(7);
    const [selectedContext, setSelectedContext] = useState<number>(2048);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const symbol = getItemSymbol(item);
    const companyName = getItemCompanyName(item);
    const selectedConfig = configs.find(config =>
        config.predictionType === selectedType
        && config.horizonLen === selectedHorizon
        && config.contextLen === selectedContext,
    );
    const hasModelConfig = (type: 'mtf-lite' | 'mtf-pro') => configs.some(config => config.predictionType === type);
    const hasHorizonConfig = (horizon: number) => configs.some(config =>
        config.predictionType === selectedType && config.horizonLen === horizon,
    );
    const hasContextConfig = (context: number) => configs.some(config =>
        config.predictionType === selectedType
        && config.horizonLen === selectedHorizon
        && config.contextLen === context,
    );
    useEffect(() => {
        if (!isOpen || !item) {
            return;
        }

        let cancelled = false;
        const preferredKey = getBacktestUniqueKey(item);
        setIsLoading(true);
        setError(null);
        setConfigs([]);

        (async () => {
            try {
                const response = await getAccessiblePredictions(undefined, symbol);
                if (cancelled) return;
                const nextConfigs = buildBacktestConfigs(flattenPublicPredictionItems(response.items || []), symbol);
                setConfigs(nextConfigs);
                const preferred = nextConfigs.find(config => config.uniqueKey === preferredKey) || nextConfigs[0];
                if (preferred) {
                    setSelectedType(preferred.predictionType);
                    setSelectedHorizon(preferred.horizonLen);
                    setSelectedContext(preferred.contextLen);
                } else {
                    setError(language === 'zh'
                        ? '暂无可用于回测的预测配置，请先执行 MTF 预测推理。'
                        : 'No backtest-ready prediction config is available. Run MTF prediction first.');
                }
            } catch (err: any) {
                if (cancelled) return;
                const message = err?.message || '';
                if (onAuthError && (
                    message.includes('Authorization header required') ||
                    message.includes('401') ||
                    message.includes('Unauthorized')
                )) {
                    onAuthError();
                    return;
                }
                setError(message || (language === 'zh' ? '加载回测配置失败' : 'Failed to load backtest configs'));
            } finally {
                if (!cancelled) {
                    setIsLoading(false);
                }
            }
        })();

        return () => {
            cancelled = true;
        };
    }, [isOpen, item?.id, symbol, language, onAuthError]);

    useEffect(() => {
        if (!configs.length) return;
        if (!hasModelConfig(selectedType)) {
            const fallback = BACKTEST_MODEL_OPTIONS.find(type => hasModelConfig(type));
            if (fallback) {
                setSelectedType(fallback);
            }
        }
    }, [configs, selectedType]);

    useEffect(() => {
        if (!configs.length) return;
        if (!hasHorizonConfig(selectedHorizon)) {
            const fallback = BACKTEST_HORIZON_OPTIONS.find(option => hasHorizonConfig(option));
            if (fallback) {
                setSelectedHorizon(fallback);
            }
        }
    }, [configs, selectedType, selectedHorizon]);

    useEffect(() => {
        if (!configs.length) return;
        if (!hasContextConfig(selectedContext)) {
            const fallback = BACKTEST_CONTEXT_OPTIONS.find(option => hasContextConfig(option));
            if (fallback) {
                setSelectedContext(fallback);
            }
        }
    }, [configs, selectedType, selectedHorizon, selectedContext]);

    if (!isOpen || !item) {
        return null;
    }

    const renderOptionButton = (
        key: string,
        label: string,
        active: boolean,
        onClick: () => void,
        isPro = false,
        disabled = false,
    ) => {
        const activeClass = isPro
            ? 'border-amber-200/45 bg-[linear-gradient(135deg,rgba(255,241,184,0.95)_0%,rgba(252,211,77,0.95)_36%,rgba(245,158,11,0.95)_72%,rgba(249,115,22,0.95)_100%)] text-[#241400] shadow-[0_10px_28px_rgba(245,158,11,0.18)]'
            : 'border-primary bg-primary text-black';
        return (
            <button
                key={key}
                type="button"
                onClick={onClick}
                disabled={isSubmitting || disabled}
                className={`rounded-lg border px-4 py-2 text-sm font-medium transition-colors ${
                    active ? activeClass : 'border-white/10 bg-white/5 text-white/70 hover:bg-white/10 hover:text-white'
                } disabled:cursor-not-allowed disabled:opacity-60`}
            >
                {label}
            </button>
        );
    };

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm" onClick={onClose}>
            <div
                className="w-full max-w-3xl overflow-hidden rounded-2xl border border-white/10 bg-card-dark shadow-2xl"
                onClick={(event) => event.stopPropagation()}
            >
                <div className="flex items-start justify-between gap-4 border-b border-white/10 px-6 py-5">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            {language === 'zh' ? '选择回测配置' : 'Select Backtest Config'}
                        </h2>
                        <p className="mt-1 text-sm text-white/55">
                            {companyName} · {symbol}
                        </p>
                    </div>
                    <button type="button" onClick={onClose} className="text-white/60 transition-colors hover:text-white">
                        <span className="material-symbols-outlined">close</span>
                    </button>
                </div>

                <div className="space-y-5 p-6">
                    {isLoading ? (
                        <div className="flex min-h-[220px] flex-col items-center justify-center gap-3 text-white/60">
                            <span className="material-symbols-outlined animate-spin text-3xl text-primary">progress_activity</span>
                            <span className="text-sm">{language === 'zh' ? '正在加载可回测配置' : 'Loading configs'}</span>
                        </div>
                    ) : (
                        <>
                            {error && (
                                <div className="rounded-xl border border-red-500/20 bg-red-500/10 p-4 text-sm text-red-300">
                                    {error}
                                </div>
                            )}

                            {configs.length > 0 && (
                                <>
                                    <section className="space-y-3">
                                        <label className="block text-sm font-medium text-white/80">
                                            {language === 'zh' ? '模型模式' : 'Model'}
                                        </label>
                                        <div className="flex flex-wrap gap-2">
                                            {BACKTEST_MODEL_OPTIONS.map(option => renderOptionButton(
                                                option,
                                                option === 'mtf-pro'
                                                    ? (language === 'zh' ? 'Pro 模型' : 'Pro Model')
                                                    : (language === 'zh' ? 'Lite 模型' : 'Lite Model'),
                                                selectedType === option,
                                                () => setSelectedType(option),
                                                option === 'mtf-pro',
                                                !hasModelConfig(option),
                                            ))}
                                        </div>
                                    </section>

                                    <section className="grid gap-5 md:grid-cols-2">
                                        <div className="space-y-3">
                                            <label className="block text-sm font-medium text-white/80">
                                                {language === 'zh' ? '预测周期' : 'Period'}
                                            </label>
                                            <div className="flex flex-wrap gap-2">
                                                {BACKTEST_HORIZON_OPTIONS.map(option => renderOptionButton(
                                                    String(option),
                                                    language === 'zh' ? `${option}天` : `${option} days`,
                                                    selectedHorizon === option,
                                                    () => setSelectedHorizon(option),
                                                    false,
                                                    !hasHorizonConfig(option),
                                                ))}
                                            </div>
                                        </div>

                                        <div className="space-y-3">
                                            <label className="block text-sm font-medium text-white/80">
                                                {language === 'zh' ? '预测深度' : 'Prediction Depth'}
                                            </label>
                                            <div className="flex flex-wrap gap-2">
                                                {BACKTEST_CONTEXT_OPTIONS.map(option => renderOptionButton(
                                                    String(option),
                                                    formatContextLen(option),
                                                    selectedContext === option,
                                                    () => setSelectedContext(option),
                                                    false,
                                                    !hasContextConfig(option),
                                                ))}
                                            </div>
                                        </div>
                                    </section>
                                </>
                            )}
                        </>
                    )}
                </div>

                <div className="flex flex-col-reverse gap-3 border-t border-white/10 px-6 py-4 sm:flex-row sm:justify-end">
                    <button
                        type="button"
                        onClick={onClose}
                        className="rounded-lg border border-white/10 bg-white/5 px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-white/10"
                    >
                        {language === 'zh' ? '取消' : 'Cancel'}
                    </button>
                    <button
                        type="button"
                        onClick={() => selectedConfig && strategy && onSubmit(selectedConfig.uniqueKey, strategy, symbol)}
                        disabled={!selectedConfig || !strategy || isLoading || isSubmitting}
                        className="flex items-center justify-center gap-2 rounded-lg bg-primary px-5 py-2.5 text-sm font-bold text-black transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        {isSubmitting && <span className="material-symbols-outlined animate-spin text-[16px]">progress_activity</span>}
                        <span>{language === 'zh' ? '开始回测' : 'Run Backtest'}</span>
                    </button>
                </div>
            </div>
        </div>
    );
};

const BacktestChartModal: React.FC<BacktestChartModalProps> = ({
    isOpen,
    result,
    title,
    configs,
    selectedUniqueKey,
    isLoading = false,
    onSelectConfig,
    onClose,
}) => {
    const { language } = useLanguage();
    const [isExpanded, setIsExpanded] = useState(false);
    const [chartLayoutMode, setChartLayoutMode] = useState<BacktestChartLayoutMode>('detail');
    const [assetViewMode, setAssetViewMode] = useState<BacktestAssetViewMode>('chart');
    const [assetTablePage, setAssetTablePage] = useState(1);

    useEffect(() => {
        if (isOpen) {
            setChartLayoutMode('detail');
            setAssetViewMode('chart');
            setAssetTablePage(1);
        }
    }, [isOpen, title]);

    useEffect(() => {
        if (isOpen) {
            setAssetTablePage(1);
        }
    }, [isOpen, selectedUniqueKey]);

    if (!isOpen) {
        return null;
    }

    const actualChange = Number(result?.actualChange) || 0;
    const assetChange = Number(result?.assetChange) || 0;
    const isOverviewMode = chartLayoutMode === 'overview';
    const chartPanelClass = `mx-auto w-full rounded-xl border border-white/10 bg-black/10 ${
        isOverviewMode ? 'max-w-[1112px] p-3' : `p-4 ${isExpanded ? 'max-w-[1600px]' : 'max-w-[1112px]'}`
    }`;
    const chartViewportClass = isOverviewMode ? 'h-[170px] sm:h-[190px] lg:h-[210px]' : (isExpanded ? 'h-[420px]' : 'h-[300px]');
    const chartDetailMode = !isOverviewMode && isExpanded ? 'expanded' : 'normal';
    const chartScrollable = !isOverviewMode;
    const isPro = result?.theme === 'pro';
    const metricCardClass = isPro
        ? 'flex min-w-[118px] flex-1 flex-col overflow-hidden rounded-lg border border-amber-300/35 bg-amber-400/[0.035]'
        : 'flex min-w-[118px] flex-1 flex-col overflow-hidden rounded-lg border border-white/10 bg-white/[0.025]';
    const metricHeaderStyle = isPro ? proMetricHeaderStyle : liteMetricHeaderStyle;
    const metricValueClass = (tone: BacktestMetric['tone']) => {
        if (tone === 'positive') return language === 'zh' ? 'text-red-300' : 'text-emerald-300';
        if (tone === 'negative') return language === 'zh' ? 'text-emerald-300' : 'text-red-300';
        return isPro ? 'text-amber-100' : 'text-white/90';
    };
    const metrics = (result?.metrics || []) as BacktestMetric[];
    const assetTablePageSize = 10;
    const assetDates = (result?.assetChartData?.dates || []) as string[];
    const assetValues = (result?.assetChartData?.actuals || []) as number[];
    const assetMarkersByIndex = new Map<number, BacktestTradeMarker[]>();
    ((result?.tradeMarkers || []) as BacktestTradeMarker[]).forEach(marker => {
        if (!assetMarkersByIndex.has(marker.index)) {
            assetMarkersByIndex.set(marker.index, []);
        }
        assetMarkersByIndex.get(marker.index)?.push(marker);
    });
    const initialAssetValue = Number(assetValues[0]);
    const assetRows = assetDates.slice(0, assetValues.length).map((date, index) => {
        const asset = Number(assetValues[index]);
        const previousAsset = index > 0 ? Number(assetValues[index - 1]) : asset;
        const dayChange = Number.isFinite(asset) && Number.isFinite(previousAsset)
            ? asset - previousAsset
            : NaN;
        const cumulativeReturn = Number.isFinite(asset) && Number.isFinite(initialAssetValue) && initialAssetValue !== 0
            ? ((asset - initialAssetValue) / initialAssetValue) * 100
            : NaN;
        const markers = assetMarkersByIndex.get(index) || [];
        const actionItems = markers.map(marker => ({
            type: marker.type,
            text: marker.type === 'buy'
                ? (language === 'zh' ? '买入' : 'Buy')
                : (language === 'zh' ? '卖出' : 'Sell'),
        }));
        const actionText = markers
            .map(marker => marker.type === 'buy'
                ? (language === 'zh' ? '买入' : 'Buy')
                : (language === 'zh' ? '卖出' : 'Sell'))
            .join(' / ');
        const tradePrices = markers.map(marker => formatBacktestNumber(marker.price, 2)).join(' / ');
        const positionAfterTrades = markers.map(marker => formatBacktestShares(marker.positionAfter)).join(' / ');
        const tradeReasons = formatTradeReasonList(markers, language);

        return {
            date,
            asset,
            dayChange,
            cumulativeReturn,
            actionItems,
            actionText,
            tradePrices,
            positionAfterTrades,
            tradeReasons,
        };
    });
    const tradeAssetRows = assetRows.filter(row => row.actionItems.length > 0);
    const totalAssetPages = Math.max(1, Math.ceil(tradeAssetRows.length / assetTablePageSize));
    const currentAssetPage = Math.min(assetTablePage, totalAssetPages);
    const visibleTradeRows = tradeAssetRows.slice(
        (currentAssetPage - 1) * assetTablePageSize,
        currentAssetPage * assetTablePageSize,
    );
    const tradeActionClass = (type: 'buy' | 'sell') => type === 'buy'
        ? 'border-red-300/30 bg-red-500/12 text-red-200'
        : 'border-emerald-300/30 bg-emerald-500/12 text-emerald-200';
    const configTabs = configs.filter(config => config.uniqueKey);
    const renderConfigTabLabel = (config: BacktestConfig) => (
        <>
            <span className={`rounded px-1.5 py-0.5 text-[10px] font-black leading-none ${
                config.predictionType === 'mtf-pro'
                    ? 'bg-amber-300/20 text-amber-100'
                    : 'bg-white/10 text-white/65'
            }`}>
                {config.predictionType === 'mtf-pro' ? 'Pro' : 'Lite'}
            </span>
            <span className="font-mono text-xs font-bold leading-none">
                {config.horizonLen > 0 ? `P${config.horizonLen}` : 'P—'}
            </span>
            <span className="font-mono text-xs leading-none text-white/65">
                {config.contextLen > 0 ? formatContextLen(config.contextLen) : '—'}
            </span>
        </>
    );
    const setAssetMode = (mode: BacktestAssetViewMode) => {
        setAssetViewMode(mode);
        setAssetTablePage(1);
    };
    const exportEquityCurve = () => {
        const dates = assetDates;
        const assets = assetValues;
        if (!dates.length || !assets.length) {
            return;
        }

        const headers = language === 'zh'
            ? ['日期', '资金值', '单日变化', '累计收益率', '交易动作', '交易价格', '交易后持股数', '预期涨跌幅', '判断值', '交易原因', '交易日期']
            : ['Date', 'Equity Value', 'Daily Change', 'Cumulative Return', 'Trade Action', 'Trade Price', 'Shares After Trade', 'Expected Change', 'Decision Value', 'Trade Reason', 'Trade Date'];
        const initialAsset = Number(assets[0]);
        const rows = dates.slice(0, assets.length).map((date, index) => {
            const asset = Number(assets[index]);
            const previousAsset = index > 0 ? Number(assets[index - 1]) : asset;
            const dayChange = Number.isFinite(asset) && Number.isFinite(previousAsset)
                ? asset - previousAsset
                : NaN;
            const cumulativeReturn = Number.isFinite(asset) && Number.isFinite(initialAsset) && initialAsset !== 0
                ? ((asset - initialAsset) / initialAsset) * 100
                : NaN;
            const markers = assetMarkersByIndex.get(index) || [];
            const actionText = markers
                .map(marker => marker.type === 'buy'
                    ? (language === 'zh' ? '买入' : 'Buy')
                    : (language === 'zh' ? '卖出' : 'Sell'))
                .join(' / ');
            const tradePrices = markers.map(marker => formatExcelNumber(marker.price, 4)).join(' / ');
            const positionAfterTrades = markers.map(marker => formatExcelShares(marker.positionAfter)).join(' / ');
            const predictedPctChanges = formatPredictedPctChangeList(markers);
            const decisionValues = formatTradeDecisionValueList(markers, {
                buyThresholdPct: result?.buyThresholdPct,
                sellThresholdPct: result?.sellThresholdPct,
            });
            const tradeReasons = formatTradeReasonList(markers, language);
            const tradeDates = markers.map(marker => marker.date || '').join(' / ');

            return [
                date,
                formatExcelNumber(asset, 4),
                formatExcelNumber(dayChange, 4),
                Number.isFinite(cumulativeReturn) ? `${cumulativeReturn.toFixed(2)}%` : '',
                actionText,
                tradePrices,
                positionAfterTrades,
                predictedPctChanges,
                decisionValues,
                tradeReasons,
                tradeDates,
            ];
        });
        const tableRows = [headers, ...rows]
            .map(row => `<tr>${row.map(cell => `<td>${escapeExcelCell(cell)}</td>`).join('')}</tr>`)
            .join('');
        const worksheetTitle = language === 'zh' ? '回测资金曲线' : 'Backtest Equity Curve';
        const html = `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>
table { border-collapse: collapse; font-family: Arial, sans-serif; font-size: 12px; }
td { border: 1px solid #999; padding: 6px 8px; mso-number-format:"\\@"; }
tr:first-child td { font-weight: 700; background: #f2f2f2; }
</style>
</head>
<body>
<h3>${escapeExcelCell(worksheetTitle)} - ${escapeExcelCell(title)}</h3>
<table>${tableRows}</table>
</body>
</html>`;
        const blob = new Blob([html], { type: 'application/vnd.ms-excel;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `${safeFileName(title)}_equity_curve.xls`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    };

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm" onClick={onClose}>
            <div
                className={`flex w-full flex-col overflow-hidden rounded-2xl border border-white/10 bg-card-dark shadow-2xl transition-all duration-200 ${
                    isExpanded ? 'h-[94vh] max-h-[94vh] max-w-[95vw]' : 'max-h-[90vh] max-w-6xl'
                }`}
                onClick={(event) => event.stopPropagation()}
            >
                <div className="flex items-start justify-between gap-4 border-b border-white/10 px-6 py-5">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            {language === 'zh' ? '回测走势' : 'Backtest Chart'}
                        </h2>
                        <p className="mt-1 text-sm text-white/55">{title}</p>
                    </div>
                    <div className="flex items-center gap-2">
                        <div className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] p-1">
                            <button
                                type="button"
                                aria-pressed={isOverviewMode}
                                onClick={() => setChartLayoutMode('overview')}
                                className={`inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-bold transition-colors ${
                                    isOverviewMode
                                        ? 'bg-primary text-black'
                                        : 'text-white/60 hover:bg-white/10 hover:text-white'
                                }`}
                                title={language === 'zh' ? '总览：收紧图表，完整显示在弹窗内' : 'Overview: fit charts inside the modal'}
                            >
                                <span className="material-symbols-outlined text-[15px] leading-none">fit_screen</span>
                                <span>{language === 'zh' ? '总览' : 'Overview'}</span>
                            </button>
                            <button
                                type="button"
                                aria-pressed={!isOverviewMode}
                                onClick={() => setChartLayoutMode('detail')}
                                className={`inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-bold transition-colors ${
                                    !isOverviewMode
                                        ? 'bg-primary text-black'
                                        : 'text-white/60 hover:bg-white/10 hover:text-white'
                                }`}
                                title={language === 'zh' ? '详情：保持当前可横向查看的细节图' : 'Detail: keep the current scrollable chart view'}
                            >
                                <span className="material-symbols-outlined text-[15px] leading-none">zoom_out_map</span>
                                <span>{language === 'zh' ? '详情' : 'Detail'}</span>
                            </button>
                        </div>
                        <button
                            type="button"
                            onClick={() => setIsExpanded(prev => !prev)}
                            className="flex h-9 w-9 items-center justify-center rounded-lg text-white/60 transition-colors hover:bg-white/10 hover:text-white"
                            title={isExpanded ? (language === 'zh' ? '还原窗口' : 'Restore') : (language === 'zh' ? '展开窗口' : 'Expand')}
                        >
                            <span className="material-symbols-outlined text-[20px]">{isExpanded ? 'fullscreen_exit' : 'fullscreen'}</span>
                        </button>
                        <button type="button" onClick={onClose} className="flex h-9 w-9 items-center justify-center rounded-lg text-white/60 transition-colors hover:bg-white/10 hover:text-white">
                            <span className="material-symbols-outlined">close</span>
                        </button>
                    </div>
                </div>

                <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
                    {configTabs.length > 0 && (
                        <section className="mx-auto w-full max-w-[1600px]">
                            <div className="flex items-center gap-3 rounded-xl border border-white/10 bg-white/[0.025] px-3 py-2">
                                <div className="flex shrink-0 items-center gap-1.5 text-xs font-semibold text-white/55">
                                    <span className="material-symbols-outlined text-[15px]">tune</span>
                                    <span>{language === 'zh' ? '配置' : 'Config'}</span>
                                </div>
                                <div className="min-w-0 flex-1 overflow-x-auto">
                                    <div className="flex w-max items-center gap-2" role="tablist" aria-label={language === 'zh' ? '回测配置' : 'Backtest configs'}>
                                        {configTabs.map((config) => {
                                            const active = config.uniqueKey === selectedUniqueKey;
                                            return (
                                                <button
                                                    key={config.uniqueKey}
                                                    type="button"
                                                    role="tab"
                                                    aria-selected={active}
                                                    disabled={isLoading && !active}
                                                    onClick={() => !active && onSelectConfig(config)}
                                                    className={`inline-flex h-9 items-center gap-2 rounded-lg border px-3 text-sm transition-colors ${
                                                        active
                                                            ? 'border-primary/60 bg-primary/15 text-primary'
                                                            : 'border-white/10 bg-white/[0.04] text-white/65 hover:bg-white/[0.08] hover:text-white'
                                                    } disabled:cursor-not-allowed disabled:opacity-45`}
                                                    title={`${config.predictionType === 'mtf-pro' ? 'Pro' : 'Lite'} · ${config.horizonLen > 0 ? `P${config.horizonLen}` : 'P—'} · ${config.contextLen > 0 ? formatContextLen(config.contextLen) : '—'}`}
                                                >
                                                    {active && isLoading ? (
                                                        <span className="material-symbols-outlined animate-spin text-[15px] leading-none">progress_activity</span>
                                                    ) : null}
                                                    {renderConfigTabLabel(config)}
                                                </button>
                                            );
                                        })}
                                    </div>
                                </div>
                                {isLoading && (
                                    <span className="hidden shrink-0 text-xs font-medium text-white/45 sm:inline">
                                        {language === 'zh' ? '加载中' : 'Loading'}
                                    </span>
                                )}
                            </div>
                        </section>
                    )}

                    {metrics.length > 0 && (
                        <section className="mx-auto w-full max-w-[1600px]">
                            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 xl:grid-cols-8">
                                {metrics.map((metric) => (
                                    <div key={metric.label} className={metricCardClass}>
                                        <div className="flex items-center justify-center gap-1.5 px-2 py-1" style={metricHeaderStyle}>
                                            <span className="material-symbols-outlined text-[13px] text-white/80">{metric.icon}</span>
                                            <span className="truncate text-xs font-medium leading-none text-white/85">{metric.label}</span>
                                        </div>
                                        <div className="flex min-h-[30px] items-center justify-center bg-white/5 px-2 py-1.5">
                                            <span className={`truncate text-sm font-black leading-tight ${metricValueClass(metric.tone)}`}>
                                                {metric.value}
                                            </span>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </section>
                    )}

                    {!result && (
                        <section className="mx-auto flex min-h-[260px] w-full max-w-[1112px] flex-col items-center justify-center rounded-xl border border-white/10 bg-black/10 p-6 text-center">
                            <span className="material-symbols-outlined text-4xl text-white/35">query_stats</span>
                            <h3 className="mt-3 text-sm font-bold text-white/80">
                                {language === 'zh' ? '当前参数暂无回测走势' : 'No Backtest Trend For This Config'}
                            </h3>
                            <p className="mt-2 max-w-md text-xs leading-5 text-white/45">
                                {language === 'zh'
                                    ? '可切换上方 P 周期与预测深度参数查看其它已生成的回测走势，或先对当前参数执行回测。'
                                    : 'Switch the P period and depth tabs above to view another generated backtest trend, or run a backtest for this config first.'}
                            </p>
                        </section>
                    )}

                    {result && <section className={chartPanelClass}>
                        <div className="mb-3 flex items-center justify-between gap-3">
                            <div>
                                <h3 className="text-sm font-bold text-white">
                                    {language === 'zh' ? '实际收盘价' : 'Validation Actual Price'}
                                </h3>
                                <p className="mt-1 text-xs text-white/45">
                                    {language === 'zh' ? '回测期间的真实走势' : 'Actual price curve during validation'}
                                </p>
                            </div>
                            <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${actualChange >= 0 ? 'bg-primary/15 text-primary' : 'bg-red-500/15 text-red-300'}`}>
                                {actualChange >= 0 ? '+' : ''}{actualChange.toFixed(2)}%
                            </span>
                        </div>
                        <div className={chartViewportClass}>
                            <PredictionChartPanel
                                change={actualChange}
                                chartData={result.actualChartData}
                                currentPrice={result.actualCurrentPrice}
                                actualLabel={language === 'zh' ? '收盘价' : 'Close'}
                                detailMode={chartDetailMode}
                                fitToContainer={isOverviewMode}
                                scrollable={chartScrollable}
                                startPrice={result.actualStartPrice}
                            />
                        </div>
                    </section>}

                    {result && <section className={chartPanelClass}>
                        <div className="mb-3 flex items-center justify-between gap-3">
                            <div>
                                <h3 className="text-sm font-bold text-white">
                                    {language === 'zh' ? '回测资产曲线' : 'Backtest Equity Curve'}
                                </h3>
                                <p className="mt-1 text-xs text-white/45">
                                    {language === 'zh' ? '策略执行后的资产变化' : 'Portfolio value after strategy execution'}
                                </p>
                            </div>
                            <div className="flex flex-wrap items-center justify-end gap-2">
                                <button
                                    type="button"
                                    onClick={() => setAssetMode(assetViewMode === 'chart' ? 'table' : 'chart')}
                                    className="inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.06] px-2.5 py-1 text-xs font-semibold text-white/70 transition-colors hover:bg-white/10 hover:text-white"
                                    title={assetViewMode === 'chart'
                                        ? (language === 'zh' ? '切换为列表' : 'Show as table')
                                        : (language === 'zh' ? '切换为图表' : 'Show as chart')}
                                >
                                    <span className="material-symbols-outlined text-[15px] leading-none">
                                        {assetViewMode === 'chart' ? 'table_rows' : 'show_chart'}
                                    </span>
                                    <span>{assetViewMode === 'chart' ? (language === 'zh' ? '列表' : 'Table') : (language === 'zh' ? '图表' : 'Chart')}</span>
                                </button>
                                <button
                                    type="button"
                                    onClick={exportEquityCurve}
                                    className="inline-flex items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.06] px-2.5 py-1 text-xs font-semibold text-white/70 transition-colors hover:bg-white/10 hover:text-white"
                                    title={language === 'zh' ? '导出资金曲线 Excel' : 'Export equity curve to Excel'}
                                >
                                    <span className="material-symbols-outlined text-[15px] leading-none">download</span>
                                    <span>{language === 'zh' ? '导出 Excel' : 'Export'}</span>
                                </button>
                                <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${assetChange >= 0 ? 'bg-primary/15 text-primary' : 'bg-red-500/15 text-red-300'}`}>
                                    {assetChange >= 0 ? '+' : ''}{assetChange.toFixed(2)}%
                                </span>
                            </div>
                        </div>
                        {assetViewMode === 'chart' ? (
                            <div className={chartViewportClass}>
                                <PredictionChartPanel
                                    change={assetChange}
                                    chartData={result.assetChartData}
                                    currentPrice={result.assetCurrentPrice}
                                    actualLabel={language === 'zh' ? '资产' : 'Equity'}
                                    detailMode={chartDetailMode}
                                    fitToContainer={isOverviewMode}
                                    markers={result.tradeMarkers}
                                    scrollable={chartScrollable}
                                    startPrice={result.assetStartPrice}
                                    theme={result.theme}
                                />
                            </div>
                        ) : (
                            <div className="overflow-hidden rounded-xl border border-white/10 bg-white/[0.025]">
                                <div className="overflow-x-auto">
                                    <table className="min-w-[1060px] w-full text-left text-sm">
                                        <thead className="bg-white/[0.055] text-xs text-white/50">
                                            <tr>
                                                <th className="px-4 py-3 font-semibold">{language === 'zh' ? '日期' : 'Date'}</th>
                                                <th className="px-4 py-3 text-right font-semibold">{language === 'zh' ? '资金值' : 'Equity'}</th>
                                                <th className="px-4 py-3 text-right font-semibold">{language === 'zh' ? '单日变化' : 'Daily Change'}</th>
                                                <th className="px-4 py-3 text-right font-semibold">{language === 'zh' ? '累计收益率' : 'Cumulative'}</th>
                                                <th className="px-4 py-3 font-semibold">{language === 'zh' ? '交易动作' : 'Action'}</th>
                                                <th className="px-4 py-3 text-right font-semibold">{language === 'zh' ? '交易价格' : 'Trade Price'}</th>
                                                <th className="px-4 py-3 text-right font-semibold">{language === 'zh' ? '交易后持股数' : 'Shares After'}</th>
                                                <th className="px-4 py-3 font-semibold">{language === 'zh' ? '交易原因' : 'Reason'}</th>
                                            </tr>
                                        </thead>
                                        <tbody className="divide-y divide-white/8">
                                            {visibleTradeRows.length > 0 ? visibleTradeRows.map((row) => {
                                                const dayPositive = row.dayChange >= 0;
                                                const returnPositive = row.cumulativeReturn >= 0;
                                                const dayClass = Number.isFinite(row.dayChange)
                                                    ? (dayPositive ? metricValueClass('positive') : metricValueClass('negative'))
                                                    : 'text-white/55';
                                                const returnClass = Number.isFinite(row.cumulativeReturn)
                                                    ? (returnPositive ? metricValueClass('positive') : metricValueClass('negative'))
                                                    : 'text-white/55';

                                                return (
                                                    <tr key={`${row.date}-${row.asset}`} className="text-white/75 transition-colors hover:bg-white/[0.035]">
                                                        <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-white/60">{row.date || '—'}</td>
                                                        <td className="whitespace-nowrap px-4 py-3 text-right font-semibold text-white/85">{formatBacktestNumber(row.asset, 2)}</td>
                                                        <td className={`whitespace-nowrap px-4 py-3 text-right font-semibold ${dayClass}`}>
                                                            {Number.isFinite(row.dayChange) ? `${dayPositive ? '+' : ''}${row.dayChange.toFixed(2)}` : '—'}
                                                        </td>
                                                        <td className={`whitespace-nowrap px-4 py-3 text-right font-semibold ${returnClass}`}>
                                                            {Number.isFinite(row.cumulativeReturn) ? `${returnPositive ? '+' : ''}${row.cumulativeReturn.toFixed(2)}%` : '—'}
                                                        </td>
                                                        <td className="px-4 py-3">
                                                            <div className="flex flex-wrap gap-1.5">
                                                                {row.actionItems.map((item, actionIndex) => (
                                                                    <span
                                                                        key={`${row.date}-${item.type}-${actionIndex}`}
                                                                        className={`rounded-full border px-2 py-1 text-xs font-semibold ${tradeActionClass(item.type)}`}
                                                                    >
                                                                        {item.text}
                                                                    </span>
                                                                ))}
                                                            </div>
                                                        </td>
                                                        <td className="whitespace-nowrap px-4 py-3 text-right font-mono text-xs text-white/70">{row.tradePrices || '—'}</td>
                                                        <td className="whitespace-nowrap px-4 py-3 text-right font-mono text-xs text-white/70">{row.positionAfterTrades || '—'}</td>
                                                        <td className="max-w-[260px] px-4 py-3 text-xs text-white/55">
                                                            <span className="line-clamp-2">{row.tradeReasons || '—'}</span>
                                                        </td>
                                                    </tr>
                                                );
                                            }) : (
                                                <tr>
                                                    <td colSpan={8} className="px-4 py-8 text-center text-sm text-white/45">
                                                        {language === 'zh' ? '暂无交易记录' : 'No trade records'}
                                                    </td>
                                                </tr>
                                            )}
                                        </tbody>
                                    </table>
                                </div>
                                <div className="flex flex-col gap-3 border-t border-white/10 px-4 py-3 text-xs text-white/50 sm:flex-row sm:items-center sm:justify-between">
                                    <span>
                                        {language === 'zh'
                                            ? `第 ${currentAssetPage} / ${totalAssetPages} 页，共 ${tradeAssetRows.length} 条`
                                            : `Page ${currentAssetPage} / ${totalAssetPages}, ${tradeAssetRows.length} rows`}
                                    </span>
                                    <div className="flex items-center gap-2">
                                        <button
                                            type="button"
                                            onClick={() => setAssetTablePage(page => Math.max(1, page - 1))}
                                            disabled={currentAssetPage <= 1}
                                            className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 font-semibold text-white/70 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-35"
                                        >
                                            <span className="material-symbols-outlined text-[15px]">chevron_left</span>
                                            <span>{language === 'zh' ? '上一页' : 'Prev'}</span>
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setAssetTablePage(page => Math.min(totalAssetPages, page + 1))}
                                            disabled={currentAssetPage >= totalAssetPages}
                                            className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 font-semibold text-white/70 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-35"
                                        >
                                            <span>{language === 'zh' ? '下一页' : 'Next'}</span>
                                            <span className="material-symbols-outlined text-[15px]">chevron_right</span>
                                        </button>
                                    </div>
                                </div>
                            </div>
                        )}
                    </section>}
                </div>
            </div>
        </div>
    );
};

const BacktestNoticeModal: React.FC<{
    notice: BacktestNotice | null;
    onClose: () => void;
    onOpenChart: () => void;
}> = ({ notice, onClose, onOpenChart }) => {
    const { language } = useLanguage();

    if (!notice) {
        return null;
    }

    const isSuccess = notice.type === 'success';
    const isError = notice.type === 'error';
    const accentClass = isSuccess
        ? 'border-primary/25 bg-primary/10 text-primary'
        : isError
            ? 'border-red-500/25 bg-red-500/10 text-red-300'
            : 'border-amber-400/25 bg-amber-400/10 text-amber-200';
    const icon = isSuccess ? 'check_circle' : isError ? 'error' : 'info';

    return (
        <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm" onClick={onClose}>
            <div
                className="w-full max-w-md overflow-hidden rounded-2xl border border-white/10 bg-card-dark shadow-2xl"
                onClick={(event) => event.stopPropagation()}
            >
                <div className="flex items-start justify-between gap-4 border-b border-white/10 px-6 py-5">
                    <div className="flex items-center gap-3">
                        <div className={`flex h-11 w-11 items-center justify-center rounded-xl border ${accentClass}`}>
                            <span className="material-symbols-outlined text-[24px]">{icon}</span>
                        </div>
                        <div>
                            <h2 className="text-lg font-bold text-white">{notice.title}</h2>
                            <p className="mt-1 text-sm text-white/55">{notice.message}</p>
                        </div>
                    </div>
                    <button type="button" onClick={onClose} className="text-white/50 transition-colors hover:text-white">
                        <span className="material-symbols-outlined">close</span>
                    </button>
                </div>

                {isSuccess && (
                    <div className="grid grid-cols-2 gap-3 px-6 py-5">
                        <div className="rounded-xl border border-white/10 bg-white/[0.04] p-4">
                            <p className="text-xs text-white/45">{language === 'zh' ? '总收益率' : 'Total Return'}</p>
                            <p className={`mt-2 text-2xl font-black ${Number(notice.totalReturn || 0) >= 0 ? 'text-primary' : 'text-red-300'}`}>
                                {Number(notice.totalReturn || 0) >= 0 ? '+' : ''}{Number(notice.totalReturn || 0).toFixed(2)}%
                            </p>
                        </div>
                        <div className="rounded-xl border border-white/10 bg-white/[0.04] p-4">
                            <p className="text-xs text-white/45">{language === 'zh' ? '交易次数' : 'Trades'}</p>
                            <p className="mt-2 text-2xl font-black text-white">{notice.tradeCount || 0}</p>
                        </div>
                    </div>
                )}

                <div className="flex items-center justify-end gap-3 border-t border-white/10 px-6 py-4">
                    <button
                        type="button"
                        onClick={onClose}
                        className="rounded-lg border border-white/10 bg-white/5 px-4 py-2 text-sm font-semibold text-white/75 transition-colors hover:bg-white/10 hover:text-white"
                    >
                        {language === 'zh' ? '关闭' : 'Close'}
                    </button>
                    {notice.canOpenChart && (
                        <button
                            type="button"
                            onClick={onOpenChart}
                            className="rounded-lg bg-primary px-4 py-2 text-sm font-bold text-black transition-opacity hover:opacity-90"
                        >
                            {language === 'zh' ? '查看回测走势' : 'View Trend'}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
};

const Portfolio: React.FC<PortfolioProps> = ({ onAuthError }) => {
    const { t, language } = useLanguage();
    const [portfolioStocks, setPortfolioStocks] = useState<StockData[]>([]);
    const [watchlistItems, setWatchlistItems] = useState<any[]>([]);
    const [userStrategies, setUserStrategies] = useState<any[]>([]);
    const [isFetching, setIsFetching] = useState(true);
    const [fetchError, setFetchError] = useState<string | null>(null);
    const [isModalOpen, setIsModalOpen] = useState(false);
    const [refreshKey, setRefreshKey] = useState(0);
    const [bindingLoading, setBindingLoading] = useState<string | null>(null); // symbol being bound
    const [backtestLoading, setBacktestLoading] = useState<string | null>(null); // symbol being backtested
    const [backtestChartLoading, setBacktestChartLoading] = useState<string | null>(null);
    const [bindModalData, setBindModalData] = useState<{symbol: string, currentKey: string | null} | null>(null);
    const [currentUser, setCurrentUser] = useState<any | null>(null);
    const [backtestResults, setBacktestResults] = useState<any | null>(null);
    const [showChart, setShowChart] = useState(false);
    const [selectedBacktestSymbol, setSelectedBacktestSymbol] = useState<string>('');
    const [selectedBacktestTitle, setSelectedBacktestTitle] = useState<string>('');
    const [selectedBacktestUniqueKey, setSelectedBacktestUniqueKey] = useState<string>('');
    const [backtestChartConfigs, setBacktestChartConfigs] = useState<BacktestConfig[]>([]);
    const [backtestResultCache, setBacktestResultCache] = useState<Record<string, any>>({});
    const [backtestConfigModal, setBacktestConfigModal] = useState<{ item: any; strategy: any } | null>(null);
    const [backtestNotice, setBacktestNotice] = useState<BacktestNotice | null>(null);
    const [bindingSearchTerm, setBindingSearchTerm] = useState('');

    const fetchWatchlist = useCallback(async () => {
        setIsFetching(true);
        setFetchError(null);
        try {
            // Parallel fetch
            const [watchlistRes, strategiesRes, profile] = await Promise.all([
                watchlistAPI.getWatchlist(),
                strategyAPI.getUserStrategies(),
                authAPI.getProfile()
            ]);

            if (strategiesRes && strategiesRes.strategies) {
                setUserStrategies(strategiesRes.strategies);
            }

            if (profile && profile.id) {
                setCurrentUser(profile);
            }

            if (watchlistRes && watchlistRes.watchlist) {
                const validWatchlistItems = watchlistRes.watchlist.filter((item: any) => !!item?.stock?.symbol);
                setWatchlistItems(validWatchlistItems);
                
                const mapped = validWatchlistItems.map(item => {
                    const strategyKey = item.strategy_unique_key || item.unique_key;
                    if (!strategyKey) return null;
                    return {
                        symbol: getItemSymbol(item),
                        uniqueKey: strategyKey,
                        companyName: getItemCompanyName(item),
                        currentPrice: item.current_price?.price || 0, 
                        changePercent: item.current_price?.change_percent || 0,
                    } as StockData;
                }).filter((item): item is StockData => item !== null);
                setPortfolioStocks(mapped);
            }
        } catch (e: any) {
            console.error("Fetch error", e);
            if (onAuthError && e.message && (
                e.message.includes('Authorization header required') || 
                e.message.includes('401') ||
                e.message.includes('Unauthorized')
            )) {
                onAuthError();
            } else {
                setFetchError(e.message || "Failed to load watchlist strategies");
            }
        } finally {
            setIsFetching(false);
        }
    }, [onAuthError]);

    useEffect(() => {
        fetchWatchlist();
    }, [fetchWatchlist, refreshKey]);

    const handleSuccess = () => {
        setRefreshKey(prev => prev + 1);
    };

    const normalizedBindingSearch = bindingSearchTerm.trim().toLowerCase();
    const filteredBindingItems = normalizedBindingSearch
        ? watchlistItems.filter(item => {
            const symbol = getItemSymbol(item).toLowerCase();
            const companyName = getItemCompanyName(item).toLowerCase();
            return symbol.includes(normalizedBindingSearch) || companyName.includes(normalizedBindingSearch);
        })
        : watchlistItems;

    const handleBind = async (symbol: string, strategyKey: string) => {
        if (!symbol || !strategyKey) return;
        setBindingLoading(symbol);
        try {
            await watchlistAPI.bindStrategy(symbol, strategyKey);
            await fetchWatchlist();
            handleSuccess();
        } catch (e: any) {
            console.error(e);
            // Optional: Show toast error
        } finally {
            setBindingLoading(null);
        }
    };

    const parseJSONB = (val: any) => {
        if (!val) return [];
        if (Array.isArray(val)) return val;
        if (typeof val === 'string') {
            try {
                if (val.startsWith('[') || val.startsWith('{')) return JSON.parse(val);
            } catch {}
            try {
                const decoded = atob(val);
                return JSON.parse(decoded);
            } catch {}
            return [];
        }
        if (typeof val === 'object') return val as any;
        return [];
    };

    const normalizeBacktest = (data: any, fallbackUniqueKey = '') => {
        const dates = parseJSONB(data.curve_dates) as string[];
        const actuals = (parseJSONB(data.actual_end_prices) as any[])
            .map((v) => Number(v))
            .filter((v) => Number.isFinite(v));
        const assets = (parseJSONB(data.equity_curve_values) as any[])
            .map((v) => Number(v))
            .filter((v) => Number.isFinite(v));
        const trades = Array.isArray(parseJSONB(data.trades)) ? parseJSONB(data.trades) as any[] : [];
        const perChunkSignals = Array.isArray(parseJSONB(data.per_chunk_signals))
            ? parseJSONB(data.per_chunk_signals) as any[]
            : [];
        const predictedPctByChunkIndex = new Map<number, number>();
        const predictedPctByDate = new Map<string, number>();
        perChunkSignals.forEach(signal => {
            const predictedPctChange = Number(signal?.predicted_pct_change);
            if (!Number.isFinite(predictedPctChange)) return;

            const chunkIndex = Number(signal?.chunk_index);
            if (Number.isFinite(chunkIndex)) {
                predictedPctByChunkIndex.set(Math.trunc(chunkIndex), predictedPctChange);
            }

            const signalDate = String(signal?.date || '').slice(0, 10);
            if (signalDate) {
                predictedPctByDate.set(signalDate, predictedPctChange);
            }
        });
        const uniqueKey = String(data.unique_key || fallbackUniqueKey || '');
        const covariateSignature = String(data.covariate_signature || '').trim();
        const theme: PredictionChartTheme = isMTFProUniqueKey(uniqueKey) || !!covariateSignature ? 'pro' : 'lite';
        const actualStart = actuals[0];
        const actualCurrent = actuals.length ? actuals[actuals.length - 1] : undefined;
        const assetStart = assets[0];
        const assetCurrent = assets.length ? assets[assets.length - 1] : undefined;
        const actualChange = Number(data.actual_total_return_pct);
        const rawAssetChange = Number(data.total_return_pct);
        const assetChange = Number.isFinite(rawAssetChange)
            ? rawAssetChange
            : (assetStart && assetCurrent ? ((assetCurrent - assetStart) / assetStart) * 100 : 0);
        const actualChangeValue = Number.isFinite(actualChange)
            ? actualChange
            : (actualStart && actualCurrent ? ((actualCurrent - actualStart) / actualStart) * 100 : 0);
        const formatPct = (value: number) => {
            if (!Number.isFinite(value)) return '—';
            return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
        };
        const formatNumber = (value: number, fractionDigits = 2) => (
            Number.isFinite(value) ? value.toFixed(fractionDigits) : '—'
        );
        const formatContext = (value: number) => {
            if (!Number.isFinite(value) || value <= 0) return '—';
            return value >= 1024 ? `${Math.round(value / 1024)}K` : String(value);
        };
        const toneForPct = (value: number): BacktestMetric['tone'] => {
            if (!Number.isFinite(value) || value === 0) return 'neutral';
            return value > 0 ? 'positive' : 'negative';
        };
        const modelName = theme === 'pro' ? 'mtf-1.5-pro' : 'mtf-1.5-lite';
        const contextLen = Number(data.context_len);
        const horizonLen = Number(data.horizon_len);
        const totalFeesPaid = Number(data.total_fees_paid);
        const excessReturn = assetChange - actualChangeValue;
        const toDateTime = (value: unknown) => {
            const normalized = String(value || '').slice(0, 10);
            const time = Date.parse(`${normalized}T00:00:00Z`);
            return Number.isFinite(time) ? time : null;
        };
        const resolveTradeIndex = (trade: any) => {
            const tradeDate = String(trade?.date || '').slice(0, 10);
            const exactIndex = dates.findIndex(date => String(date || '').slice(0, 10) === tradeDate);
            if (exactIndex >= 0) return exactIndex;

            const tradeTime = toDateTime(tradeDate);
            if (tradeTime !== null) {
                const nextIndex = dates.findIndex(date => {
                    const curveTime = toDateTime(date);
                    return curveTime !== null && curveTime >= tradeTime;
                });
                if (nextIndex >= 0) return nextIndex;
            }

            const chunkIndex = Number(trade?.chunk_index);
            if (Number.isFinite(chunkIndex) && assets.length > 0) {
                return Math.min(Math.max(Math.trunc(chunkIndex), 0), assets.length - 1);
            }
            return null;
        };
        let runningPosition = 0;
        const tradeMarkers: BacktestTradeMarker[] = trades
            .map((trade) => {
                const action = String(trade?.action || '').toLowerCase();
                const index = resolveTradeIndex(trade);
                const size = normalizeBacktestShareLot(trade?.size);
                if (index === null || (action !== 'buy' && action !== 'sell')) {
                    return null;
                }
                if (size !== undefined) {
                    runningPosition += action === 'buy' ? size : -size;
                    if (runningPosition > -0.000001 && runningPosition < 0) {
                        runningPosition = 0;
                    }
                }
                const positionAfter = size !== undefined
                    ? normalizeBacktestShareLot(Math.max(0, runningPosition))
                    : undefined;
                return {
                    index,
                    type: action as 'buy' | 'sell',
                    price: Number(trade?.price),
                    positionAfter,
                    size,
                    predictedPctChange: predictedPctByChunkIndex.get(Math.trunc(Number(trade?.chunk_index)))
                        ?? predictedPctByDate.get(String(trade?.date || '').slice(0, 10)),
                    date: String(trade?.date || ''),
                    reason: String(trade?.reason || ''),
                };
            })
            .filter((marker): marker is BacktestTradeMarker => marker !== null);

        return {
            theme,
            tradeMarkers,
            buyThresholdPct: Number(data.buy_threshold_pct),
            sellThresholdPct: Number(data.sell_threshold_pct),
            actualChange: actualChangeValue,
            assetChange,
            metrics: [
                {
                    icon: 'smart_toy',
                    label: language === 'zh' ? '模型' : 'Model',
                    value: modelName,
                },
                {
                    icon: 'memory',
                    label: language === 'zh' ? '预测深度' : 'Depth',
                    value: formatContext(contextLen),
                },
                {
                    icon: 'calendar_today',
                    label: language === 'zh' ? '预测周期' : 'Period',
                    value: horizonLen > 0 ? `${horizonLen}${language === 'zh' ? '天' : 'd'}` : '—',
                },
                {
                    icon: 'show_chart',
                    label: language === 'zh' ? '策略收益' : 'Strategy',
                    value: formatPct(assetChange),
                    tone: toneForPct(assetChange),
                },
                {
                    icon: 'trending_up',
                    label: language === 'zh' ? '真实涨跌' : 'Actual',
                    value: formatPct(actualChangeValue),
                    tone: toneForPct(actualChangeValue),
                },
                {
                    icon: 'compare_arrows',
                    label: language === 'zh' ? '超额收益' : 'Excess',
                    value: formatPct(excessReturn),
                    tone: toneForPct(excessReturn),
                },
                {
                    icon: 'swap_vert',
                    label: language === 'zh' ? '交易次数' : 'Trades',
                    value: String(trades.length),
                },
                {
                    icon: 'payments',
                    label: language === 'zh' ? '手续费' : 'Fees',
                    value: formatNumber(totalFeesPaid, 2),
                },
            ] as BacktestMetric[],
            actualChartData: {
                dates: dates.slice(0, actuals.length),
                actuals,
                predictions: [],
            },
            assetChartData: {
                dates: dates.slice(0, assets.length),
                actuals: assets,
                predictions: [],
            },
            actualCurrentPrice: actualCurrent,
            actualStartPrice: actualStart,
            assetCurrentPrice: assetCurrent,
            assetStartPrice: assetStart,
        };
    };

    const loadBacktestConfigsForSymbol = async (stockSymbol: string) => {
        const response = await getAccessiblePredictions(undefined, stockSymbol);
        return buildBacktestConfigs(flattenPublicPredictionItems(response.items || []), stockSymbol);
    };

    const loadBacktestChartResult = async (uniqueKey: string, stockSymbol?: string, title?: string) => {
        if (!uniqueKey) {
            return false;
        }
        if (stockSymbol) {
            setSelectedBacktestSymbol(stockSymbol);
            setSelectedBacktestTitle(title || stockSymbol);
        }
        const cached = backtestResultCache[uniqueKey];
        if (cached) {
            setBacktestResults(cached);
            return true;
        }

        try {
            const res = await backtestAPI.getByUniqueKey(uniqueKey);
            if (res) {
                const normalized = normalizeBacktest(res, uniqueKey);
                setBacktestResults(normalized);
                setBacktestResultCache(prev => ({
                    ...prev,
                    [uniqueKey]: normalized,
                }));
                return true;
            }
            return false;
        } catch {
            return false;
        }
    };

    const handleBacktest = async (uniqueKey: string, strategy: any, stockSymbol: string) => {
        if (!uniqueKey || !strategy) return;
        setBacktestLoading(stockSymbol);
        try {
            const req = {
                unique_key: uniqueKey,
                user_id: currentUser?.id,
                strategy_params_id: strategy.id,
                buy_threshold_pct: strategy.buy_threshold_pct,
                sell_threshold_pct: strategy.sell_threshold_pct,
                initial_cash: strategy.initial_cash,
                enable_rebalance: strategy.enable_rebalance,
                max_position_pct: strategy.max_position_pct,
                min_position_pct: strategy.min_position_pct,
                slope_position_per_pct: strategy.slope_position_per_pct,
                rebalance_tolerance_pct: strategy.rebalance_tolerance_pct,
                trade_fee_rate: strategy.trade_fee_rate,
                take_profit_threshold_pct: strategy.take_profit_threshold_pct,
                take_profit_sell_frac: strategy.take_profit_sell_frac,
            };
            const res = await backtestAPI.runBacktest(req);
            if (res.success) {
                 const totalReturn = Number(res.backtest?.total_return_pct || 0);
                 const tradeCount = res.backtest?.trades?.length || 0;
                 const norm = normalizeBacktest(res.backtest || {}, uniqueKey);
                 setSelectedBacktestSymbol(stockSymbol);
                 setSelectedBacktestTitle(stockSymbol);
                 setSelectedBacktestUniqueKey(uniqueKey);
                 setBacktestResults(norm);
                 setBacktestResultCache(prev => ({
                    ...prev,
                    [uniqueKey]: norm,
                 }));
                 try {
                    const configs = await loadBacktestConfigsForSymbol(stockSymbol);
                    setBacktestChartConfigs(configs);
                 } catch {
                    setBacktestChartConfigs([]);
                 }
                 setBacktestNotice({
                    type: 'success',
                    title: language === 'zh' ? '回测完成' : 'Backtest Finished',
                    message: language === 'zh' ? '策略回测已生成，可继续查看曲线。' : 'The strategy backtest is ready. You can view the curve now.',
                    totalReturn,
                    tradeCount,
                    canOpenChart: true,
                 });
            } else {
                 setBacktestNotice({
                    type: 'error',
                    title: language === 'zh' ? '回测失败' : 'Backtest Failed',
                    message: res.error || res.message || (language === 'zh' ? '请稍后重试。' : 'Please try again later.'),
                 });
            }
        } catch (e: any) {
            console.error(e);
            setBacktestNotice({
                type: 'error',
                title: language === 'zh' ? '回测错误' : 'Backtest Error',
                message: e.message || (language === 'zh' ? '请求未完成，请稍后重试。' : 'The request did not finish. Please try again later.'),
            });
        } finally {
            setBacktestLoading(null);
        }
    };

    const handleConfiguredBacktest = async (uniqueKey: string, strategy: any, stockSymbol: string) => {
        setBacktestConfigModal(null);
        await handleBacktest(uniqueKey, strategy, stockSymbol);
    };

    const openBacktestAction = (item: any, strategy: any) => {
        setBacktestConfigModal({ item, strategy });
    };

    const openBacktestChartAction = async (item: any, symbol: string, companyName: string) => {
        if (!symbol) return;

        const uniqueKey = getBacktestUniqueKey(item);
        if (!uniqueKey) {
            setBacktestNotice({
                type: 'info',
                title: language === 'zh' ? '暂无回测走势' : 'No Backtest Trend',
                message: language === 'zh' ? '请先执行回测。' : 'Run backtest first.',
            });
            return;
        }

        setBacktestChartLoading(symbol);
        try {
            let configs: BacktestConfig[] = [];
            try {
                configs = await loadBacktestConfigsForSymbol(symbol);
            } catch (err: any) {
                const message = err?.message || '';
                if (onAuthError && (
                    message.includes('Authorization header required') ||
                    message.includes('401') ||
                    message.includes('Unauthorized')
                )) {
                    onAuthError();
                    return;
                }
            }

            setBacktestChartConfigs(configs);
            const selectedConfig = configs.find(config => config.uniqueKey === uniqueKey) || configs[0];
            const targetUniqueKey = selectedConfig?.uniqueKey || uniqueKey;
            const orderedConfigs = [
                ...(selectedConfig ? [selectedConfig] : []),
                ...configs.filter(config => config.uniqueKey !== targetUniqueKey),
            ];
            if (!orderedConfigs.length) {
                orderedConfigs.push({
                    uniqueKey: targetUniqueKey,
                    predictionType: isMTFProUniqueKey(targetUniqueKey) ? 'mtf-pro' : 'mtf-lite',
                    horizonLen: 0,
                    contextLen: 0,
                    mtfVersion: '2.5',
                    chunkCount: 0,
                });
            }

            setSelectedBacktestSymbol(symbol);
            setSelectedBacktestTitle(`${companyName || symbol} · ${symbol}`);
            let hasResult = false;
            for (const config of orderedConfigs) {
                setSelectedBacktestUniqueKey(config.uniqueKey);
                hasResult = await loadBacktestChartResult(config.uniqueKey, symbol, `${companyName || symbol} · ${symbol}`);
                if (hasResult) break;
            }
            if (!hasResult) {
                setSelectedBacktestUniqueKey(targetUniqueKey);
                setBacktestResults(null);
            }
            setShowChart(true);
        } finally {
            setBacktestChartLoading(null);
        }
    };

    const handleSelectBacktestChartConfig = async (config: BacktestConfig) => {
        if (!config.uniqueKey || config.uniqueKey === selectedBacktestUniqueKey) {
            return;
        }

        setSelectedBacktestUniqueKey(config.uniqueKey);
        setBacktestChartLoading(selectedBacktestSymbol || config.uniqueKey);
        try {
            const has = await loadBacktestChartResult(
                config.uniqueKey,
                selectedBacktestSymbol,
                selectedBacktestTitle || selectedBacktestSymbol,
            );
            if (!has) {
                setBacktestResults(null);
            }
        } finally {
            setBacktestChartLoading(null);
        }
    };

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-wrap justify-between gap-4 items-center">
                <div className="flex flex-col gap-1">
                    <h1 className="text-white text-4xl font-black leading-tight tracking-[-0.033em]">
                        {language === 'zh' ? '投资组合策略' : 'Portfolio Strategies'}
                    </h1>
                    <p className="text-white/60 text-base font-normal leading-normal">
                        {language === 'zh' ? '管理您关注列表中的自动化投资策略' : 'Manage automated investment strategies for your watchlist'}
                    </p>
                </div>
            </header>

            {/* Strategy Section */}
            {isFetching ? (
                 <div className="flex items-center justify-center h-64">
                    <span className="material-symbols-outlined animate-spin text-4xl text-primary">progress_activity</span>
                 </div>
            ) : fetchError ? (
                <div 
                    onClick={onAuthError}
                    className={`p-4 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 ${onAuthError ? 'cursor-pointer hover:bg-red-500/20 transition-colors' : ''}`}
                >
                    {fetchError}
                </div>
            ) : (
                <div className="space-y-8">
                    {/* Official Strategies */}
                    {userStrategies.some(s => s.is_public === 1) && (
                        <div className="space-y-3">
                            <h3 className="text-white/60 text-sm font-medium uppercase tracking-wider">{language === 'zh' ? '官方推荐' : 'Official Recommended'}</h3>
                            <div className="grid grid-cols-3 gap-2 sm:gap-3 lg:gap-4">
                                {userStrategies
                                    .filter(s => s.is_public === 1)
                                    .map((strategy) => (
                                        <CompactStrategyCard
                                            key={`strat-${strategy.unique_key}`}
                                            strategy={strategy}
                                            language={language}
                                        />
                                    ))
                                }
                            </div>
                        </div>
                    )}

                    {/* Personal Strategies */}
                    <div className="space-y-3">
                        <h3 className="text-white/60 text-sm font-medium uppercase tracking-wider">{language === 'zh' ? '个人策略' : 'My Strategies'}</h3>
                        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {userStrategies
                                .filter(s => s.is_public !== 1)
                                .map((strategy) => (
                                    <StrategyCard 
                                        key={`strat-${strategy.unique_key}`} 
                                        uniqueKey={strategy.unique_key} 
                                        symbol={strategy.name || 'Strategy'}
                                    />
                                ))
                            }
                            
                            <button
                                onClick={() => setIsModalOpen(true)}
                                className="rounded-xl bg-white/5 border border-dashed border-white/20 hover:border-primary/50 hover:bg-white/10 transition-all p-5 flex flex-col items-center justify-center h-full min-h-[200px] group"
                            >
                                <div className="w-12 h-12 rounded-full bg-primary/10 text-primary flex items-center justify-center mb-3 group-hover:scale-110 transition-transform">
                                    <span className="material-symbols-outlined text-2xl">add</span>
                                </div>
                                <span className="text-white font-bold text-lg">{language === 'zh' ? '添加策略' : 'Add Strategy'}</span>
                                <p className="text-white/40 text-xs mt-1">{language === 'zh' ? '为新股票配置策略' : 'Configure strategy for new stock'}</p>
                            </button>
                        </div>
                    </div>
                </div>
            )}
            
            {/* Watchlist Binding Section */}
            <div className="bg-[#1E1E1E] rounded-xl border border-white/10 overflow-hidden">
                <div className="flex flex-col gap-4 border-b border-white/10 p-4 sm:p-6 md:flex-row md:items-center md:justify-between">
                    <div>
                        <h2 className="text-xl font-bold text-white">
                            {language === 'zh' ? '策略绑定' : 'Strategy Bindings'}
                        </h2>
                        <p className="text-white/60 text-sm mt-1">
                            {language === 'zh' ? '将策略应用到关注列表中的股票' : 'Apply strategies to stocks in your watchlist'}
                        </p>
                    </div>
                    <label className="flex h-11 w-full items-center gap-2 rounded-lg border border-white/10 bg-white/5 px-3 transition-colors focus-within:border-primary/70 focus-within:bg-white/[0.07] md:w-72">
                        <span className="material-symbols-outlined text-[18px] text-white/45">search</span>
                        <input
                            value={bindingSearchTerm}
                            onChange={(event) => setBindingSearchTerm(event.target.value)}
                            placeholder={language === 'zh' ? '搜索股票代码或名称' : 'Search ticker or name'}
                            className="h-full min-w-0 flex-1 border-0 bg-transparent text-sm text-white outline-none placeholder:text-white/35 focus:border-0 focus:outline-none focus:ring-0"
                        />
                        {bindingSearchTerm && (
                            <button
                                type="button"
                                onClick={() => setBindingSearchTerm('')}
                                className="flex h-7 w-7 items-center justify-center rounded-md text-white/45 transition-colors hover:bg-white/10 hover:text-white"
                                aria-label={language === 'zh' ? '清空搜索' : 'Clear search'}
                            >
                                <span className="material-symbols-outlined text-[16px]">close</span>
                            </button>
                        )}
                    </label>
                </div>

                <div className="space-y-3 p-4 md:hidden">
                    {filteredBindingItems.length > 0 ? filteredBindingItems.map((item) => {
                        const currentStrategy = userStrategies.find(s => s.unique_key === item.strategy_unique_key);
                        const symbol = getItemSymbol(item);
                        const companyName = getItemCompanyName(item);
                        const isBinding = bindingLoading === symbol;
                        const canRunBacktest = !!currentStrategy;

                        return (
                            <div key={item.id} className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0">
                                        <p className="font-mono text-sm font-semibold text-white">{symbol || '—'}</p>
                                        <p className="mt-1 text-sm text-white/55">{companyName || '—'}</p>
                                    </div>
                                    {currentStrategy ? (
                                        <span className="shrink-0 rounded-full bg-primary/12 px-2.5 py-1 text-[11px] font-medium text-primary">
                                            {currentStrategy.is_public === 1
                                                ? (language === 'zh' ? '官方策略' : 'Official')
                                                : (language === 'zh' ? '个人策略' : 'Personal')}
                                        </span>
                                    ) : (
                                        <span className="shrink-0 rounded-full bg-white/6 px-2.5 py-1 text-[11px] font-medium text-white/45">
                                            {language === 'zh' ? '未绑定' : 'Unbound'}
                                        </span>
                                    )}
                                </div>

                                <div className="mt-4 rounded-xl border border-white/8 bg-black/10 p-3">
                                    <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-white/35">
                                        {language === 'zh' ? '当前策略' : 'Current Strategy'}
                                    </p>
                                    {currentStrategy ? (
                                        <div className="mt-2">
                                            <p className="text-sm font-medium text-white">{currentStrategy.name || 'Unnamed Strategy'}</p>
                                            <p className="mt-1 text-xs text-white/45 font-mono">
                                                {currentStrategy.buy_threshold_pct}% | {currentStrategy.sell_threshold_pct}%
                                            </p>
                                        </div>
                                    ) : (
                                        <p className="mt-2 text-sm text-white/45">
                                            {language === 'zh' ? '当前未绑定任何策略' : 'No strategy bound yet'}
                                        </p>
                                    )}
                                </div>

                                <div className="mt-4 flex flex-wrap gap-2">
                                    {canRunBacktest && (
                                        <button
                                            onClick={() => openBacktestAction(item, currentStrategy)}
                                            disabled={backtestLoading === symbol}
                                            className="flex min-w-[120px] flex-1 items-center justify-center rounded-xl border border-purple-500/20 bg-purple-500/10 px-3 py-2 text-sm font-medium text-purple-300 transition-colors hover:bg-purple-500/15"
                                        >
                                            {backtestLoading === symbol
                                                ? (language === 'zh' ? '回测中...' : 'Backtesting...')
                                                : (language === 'zh' ? '回测' : 'Backtest')}
                                        </button>
                                    )}
                                    <button
                                        onClick={() => openBacktestChartAction(item, symbol, companyName)}
                                        disabled={backtestChartLoading === symbol || !symbol}
                                        className="flex min-w-[120px] flex-1 items-center justify-center rounded-xl border border-amber-400/20 bg-amber-400/10 px-3 py-2 text-sm font-medium text-amber-200 transition-colors hover:bg-amber-400/15 disabled:cursor-not-allowed disabled:opacity-50"
                                    >
                                        {backtestChartLoading === symbol
                                            ? (language === 'zh' ? '加载中...' : 'Loading...')
                                            : (language === 'zh' ? '回测走势' : 'Backtest Trend')}
                                    </button>
                                    <button
                                        onClick={() => setBindModalData({
                                            symbol,
                                            currentKey: item.strategy_unique_key || null
                                        })}
                                        disabled={isBinding || !symbol}
                                        className="flex min-w-[120px] flex-1 items-center justify-center rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-white/10"
                                    >
                                        {isBinding
                                            ? (language === 'zh' ? '处理中...' : 'Processing...')
                                            : (item.strategy_unique_key
                                                ? (language === 'zh' ? '更换策略' : 'Change Strategy')
                                                : (language === 'zh' ? '绑定策略' : 'Bind Strategy'))}
                                    </button>
                                </div>

                                {!currentStrategy && (
                                    <p className="mt-3 text-xs text-white/35">
                                        {language === 'zh' ? '请先绑定策略后再回测。' : 'Bind a strategy before backtesting.'}
                                    </p>
                                )}
                            </div>
                        );
                    }) : (
                        <div className="rounded-2xl border border-white/10 bg-white/[0.03] px-4 py-8 text-center text-sm text-white/40">
                            {watchlistItems.length === 0
                                ? (language === 'zh' ? '关注列表为空' : 'Watchlist is empty')
                                : (language === 'zh' ? '没有匹配的股票' : 'No matching stocks')}
                        </div>
                    )}
                </div>

                <div className="hidden overflow-x-auto overscroll-x-contain md:block touch-pan-x">
                    <table className="w-full min-w-[760px] text-left text-sm text-white/60">
                        <thead className="bg-white/5 text-xs uppercase font-semibold text-white/40">
                            <tr>
                                <th className="px-6 py-4">{language === 'zh' ? '股票' : 'Stock'}</th>
                                <th className="px-6 py-4">{language === 'zh' ? '当前策略' : 'Current Strategy'}</th>
                                <th className="px-6 py-4">{language === 'zh' ? '操作' : 'Action'}</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-white/5">
                            {filteredBindingItems.map((item) => {
                                const currentStrategy = userStrategies.find(s => s.unique_key === item.strategy_unique_key);
                                const symbol = getItemSymbol(item);
                                const companyName = getItemCompanyName(item);
                                const isBinding = bindingLoading === symbol;
                                const canRunBacktest = !!currentStrategy;
                                
                                return (
                                    <tr key={item.id} className="hover:bg-white/5 transition-colors">
                                        <td className="px-6 py-4">
                                            <div className="flex flex-col">
                                                <span className="text-white font-medium font-mono">{symbol || '—'}</span>
                                                <span className="text-xs">{companyName || '—'}</span>
                                            </div>
                                        </td>
                                        <td className="px-6 py-4">
                                            {currentStrategy ? (
                                                <div className="flex items-center gap-2">
                                                    <span className="text-primary font-medium">{currentStrategy.name || 'Unnamed Strategy'}</span>
                                                    {currentStrategy.is_public === 1 ? (
                                                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-400 font-medium whitespace-nowrap">
                                                            {language === 'zh' ? '官方' : 'Official'}
                                                        </span>
                                                    ) : (
                                                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-purple-500/20 text-purple-400 font-medium whitespace-nowrap">
                                                            {language === 'zh' ? '个人' : 'Personal'}
                                                        </span>
                                                    )}
                                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-primary/10 text-primary/80 font-mono">
                                                        {currentStrategy.buy_threshold_pct}%|{currentStrategy.sell_threshold_pct}%
                                                    </span>
                                                </div>
                                            ) : (
                                                <span className="text-white/20 italic">{language === 'zh' ? '未绑定' : 'Unbound'}</span>
                                            )}
                                        </td>
                                        <td className="px-6 py-4">
                                            <div className="flex items-center gap-2">
                                                <button
                                                    onClick={() => canRunBacktest && openBacktestAction(item, currentStrategy)}
                                                    disabled={!canRunBacktest || backtestLoading === symbol}
                                                    title={!currentStrategy ? (language === 'zh' ? '请先绑定策略后再回测' : 'Bind a strategy before backtesting.') : undefined}
                                                    className={`flex items-center justify-center gap-1 rounded-md border px-3 py-1.5 text-center text-xs font-medium transition-all ${
                                                        canRunBacktest
                                                            ? 'bg-purple-500/10 border-purple-500/20 text-purple-400 hover:bg-purple-500/20 hover:border-purple-500/30'
                                                            : 'bg-white/5 border-white/10 text-white/25 cursor-not-allowed'
                                                    }`}
                                                >
                                                    {backtestLoading === symbol && <span className="material-symbols-outlined animate-spin text-[10px]">progress_activity</span>}
                                                    {language === 'zh' ? '回测' : 'Backtest'}
                                                </button>
                                                <button
                                                    onClick={() => openBacktestChartAction(item, symbol, companyName)}
                                                    disabled={backtestChartLoading === symbol || !symbol}
                                                    className="flex min-w-[86px] items-center justify-center gap-1 rounded-md border border-amber-400/20 bg-amber-400/10 px-3 py-1.5 text-center text-xs font-medium text-amber-200 transition-all hover:border-amber-400/30 hover:bg-amber-400/15 disabled:cursor-not-allowed disabled:opacity-50"
                                                >
                                                    {backtestChartLoading === symbol && <span className="material-symbols-outlined animate-spin text-[10px]">progress_activity</span>}
                                                    {language === 'zh' ? '回测走势' : 'Trend'}
                                                </button>
                                                <button
                                                    onClick={() => setBindModalData({
                                                        symbol,
                                                        currentKey: item.strategy_unique_key || null
                                                    })}
                                                    disabled={isBinding || !symbol}
                                                    className="flex items-center justify-center gap-1 rounded-md border border-white/10 bg-white/5 px-3 py-1.5 text-center text-xs font-medium text-white transition-all hover:border-white/20 hover:bg-white/10"
                                                >
                                                    {item.strategy_unique_key
                                                        ? (language === 'zh' ? '换绑' : 'Change')
                                                        : (language === 'zh' ? '绑定' : 'Bind')}
                                                </button>
                                                {isBinding && <span className="material-symbols-outlined animate-spin text-primary text-sm">progress_activity</span>}
                                            </div>
                                        </td>
                                    </tr>
                                );
                            })}
                            {filteredBindingItems.length === 0 && (
                                <tr>
                                    <td colSpan={3} className="px-6 py-8 text-center text-white/40">
                                        {watchlistItems.length === 0
                                            ? (language === 'zh' ? '关注列表为空' : 'Watchlist is empty')
                                            : (language === 'zh' ? '没有匹配的股票' : 'No matching stocks')}
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            </div>

            <BindStrategyModal
                isOpen={!!bindModalData}
                onClose={() => setBindModalData(null)}
                symbol={bindModalData?.symbol || ''}
                strategies={userStrategies}
                currentStrategyKey={bindModalData?.currentKey || undefined}
                onBind={handleBind}
            />

            <CreateStrategyModal 
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                onSuccess={handleSuccess}
            />

            <BacktestConfigModal
                isOpen={!!backtestConfigModal}
                item={backtestConfigModal?.item || null}
                strategy={backtestConfigModal?.strategy || null}
                isSubmitting={!!backtestLoading}
                onClose={() => setBacktestConfigModal(null)}
                onSubmit={handleConfiguredBacktest}
                onAuthError={onAuthError}
            />

            <BacktestChartModal
                isOpen={showChart}
                result={backtestResults}
                title={selectedBacktestTitle || selectedBacktestSymbol}
                configs={backtestChartConfigs}
                selectedUniqueKey={selectedBacktestUniqueKey}
                isLoading={!!backtestChartLoading}
                onSelectConfig={handleSelectBacktestChartConfig}
                onClose={() => setShowChart(false)}
            />

            <BacktestNoticeModal
                notice={backtestNotice}
                onClose={() => setBacktestNotice(null)}
                onOpenChart={() => {
                    setBacktestNotice(null);
                    setShowChart(true);
                }}
            />
        </div>
    );
};

export default Portfolio;
