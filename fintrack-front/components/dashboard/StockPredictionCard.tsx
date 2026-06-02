
import React, { useState } from 'react';
import { StockData } from '../../types';
import { useLanguage } from '../../contexts/LanguageContext';
import { getChangeColors } from '../../utils/colorUtils';
import { PredictionChartMode } from '../common/PredictionChart';
import PredictionChartPanel from '../common/PredictionChartPanel';

interface StockPredictionCardProps {
  stock: StockData;
  onAddToWatchlist?: (symbol: string, type: 1 | 2) => void | Promise<void>;
  className?: string;
  chartHeightClassName?: string;
  borderless?: boolean;
  hideSummary?: boolean;
  chartMode?: PredictionChartMode;
  onChartModeChange?: (mode: PredictionChartMode) => void;
  horizonOptions?: Array<{ value: number; available: boolean }>;
  selectedHorizon?: number;
  onHorizonChange?: (value: number) => void;
}

const liteHeaderStyle = {
    backgroundColor: 'rgba(226, 232, 240, 0.14)',
};

const proHeaderGradientStyle = {
    backgroundImage: 'linear-gradient(90deg, rgba(255,241,184,0.75) 0%, rgba(252,211,77,0.75) 34%, rgba(245,158,11,0.75) 68%, rgba(249,115,22,0.75) 100%)',
};

const liteMetricCardClass = 'flex flex-col rounded-lg border border-white/10 overflow-hidden h-fit w-full md:w-[100px] md:shrink-0';
const proMetricCardClass = 'flex flex-col rounded-lg border border-amber-300/35 overflow-hidden h-fit w-full md:w-[100px] md:shrink-0';
const metricsRowClass = 'grid grid-cols-3 gap-1.5 md:flex md:gap-2 md:overflow-x-auto no-scrollbar';

interface StockPredictionSummaryProps {
    stock: StockData;
    onAddToWatchlist?: (symbol: string, type: 1 | 2) => void | Promise<void>;
    chartMode: PredictionChartMode;
    onChartModeChange: (mode: PredictionChartMode) => void;
    horizonOptions?: Array<{ value: number; available: boolean }>;
    selectedHorizon?: number;
    onHorizonChange?: (value: number) => void;
    className?: string;
    nameColumnClassName?: string;
    showChartModeToggle?: boolean;
}

export const StockPredictionSummary: React.FC<StockPredictionSummaryProps> = ({
    stock,
    onAddToWatchlist,
    chartMode,
    onChartModeChange,
    horizonOptions,
    selectedHorizon,
    onHorizonChange,
    className = '',
    nameColumnClassName = 'md:min-w-[140px] md:max-w-[140px] md:flex-[0_0_140px] lg:min-w-[180px] lg:max-w-[180px] lg:flex-[0_0_180px]',
    showChartModeToggle = true,
}) => {
    const { language, t } = useLanguage();
    const isPositive = stock.changePercent >= 0;
    const { hexColor: actualChangeColor } = getChangeColors(isPositive, language);
    const actualChangeText = `${isPositive ? '+' : '-'}${Math.abs(stock.changePercent).toFixed(2)}%`;

    const isPredPositive = (stock.predictedChangePercent || 0) >= 0;
    const isProPredPositive = (stock.prediction?.proPredictedChangePercent || 0) >= 0;
    const formatContextLen = (value?: number) => (
        value ? (value < 1024 ? value : Math.round(value / 1024) + 'K') : '?'
    );

    const confidenceColor = stock.prediction?.confidence ?? 0 > 85 ? 'text-primary' : (stock.prediction?.confidence ?? 0) > 70 ? 'text-yellow-400' : 'text-red-400';
    const proConfidenceColor = stock.prediction?.proConfidence ?? 0 > 85 ? 'text-amber-200' : (stock.prediction?.proConfidence ?? 0) > 70 ? 'text-yellow-200' : 'text-orange-300';

    const [isAddingToWatchlist, setIsAddingToWatchlist] = useState(false);
    const [isMetricsExpanded, setIsMetricsExpanded] = useState(false);

    const hasLiteMetrics = Boolean(stock.prediction?.modelName);
    const hasProMetrics = Boolean(
        stock.prediction?.proModelName
        || stock.prediction?.proConfidence !== undefined
        || stock.prediction?.proMaxDeviationPercent !== undefined
        || stock.prediction?.proPredictedChangePercent !== undefined,
    );
    const hasMetrics = hasLiteMetrics || hasProMetrics;
    const hasChartData = Boolean(stock.prediction?.chartData);
    const hasHorizonOptions = Boolean(horizonOptions?.length);
    const isChangeMode = chartMode === 'change';
    const chartModeLabel = isChangeMode
        ? (language === 'zh' ? '价格' : 'Price')
        : (language === 'zh' ? '涨跌幅' : 'Change');
    const chartModeTitle = isChangeMode
        ? (language === 'zh' ? '切换到价格走势' : 'Show price trend')
        : (language === 'zh' ? '切换到涨跌幅对比' : 'Show change comparison');
    const watchlistCount = typeof stock.watchlistCount === 'number' ? stock.watchlistCount : undefined;
    const watchlistCountLabel = t('prediction.watchlistCount');
    const formattedWatchlistCount = watchlistCount !== undefined
        ? new Intl.NumberFormat(language === 'zh' ? 'zh-CN' : 'en-US', {
            notation: watchlistCount >= 10000 ? 'compact' : 'standard',
            maximumFractionDigits: 1,
        }).format(watchlistCount)
        : '';
    const isWatchlisted = Boolean(stock.isWatchlisted);
    const watchlistTitle = isWatchlisted
        ? t('dashboard.addedToWatchlist')
        : t('dashboard.addToWatchlist');
    const handleWatchlistClick = async () => {
        if (!onAddToWatchlist || isWatchlisted || isAddingToWatchlist) {
            return;
        }
        try {
            setIsAddingToWatchlist(true);
            await onAddToWatchlist(stock.symbol, stock.stockType || 1);
        } finally {
            setIsAddingToWatchlist(false);
        }
    };

    return (
        <div className={`flex flex-wrap md:flex-nowrap justify-between items-start gap-x-2 gap-y-3 ${className}`.trim()}>
            <div className={`min-w-0 ${nameColumnClassName}`.trim()}>
                <p className="text-white text-lg font-bold leading-normal md:truncate">{stock.companyName}</p>
                <p className="text-white/60 text-sm truncate max-w-[150px]">{stock.symbol}</p>
                {showChartModeToggle && hasChartData && (
                    <button
                        type="button"
                        aria-label={chartModeTitle}
                        title={chartModeTitle}
                        onClick={() => onChartModeChange(chartMode === 'change' ? 'price' : 'change')}
                        className="mt-3 inline-flex h-7 items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.06] px-2.5 text-[11px] font-semibold text-white/70 shadow-sm transition-colors hover:bg-white/10 hover:text-white"
                    >
                        <span className="material-symbols-outlined text-[16px] leading-none">
                            {isChangeMode ? 'show_chart' : 'percent'}
                        </span>
                        <span>{chartModeLabel}</span>
                    </button>
                )}
            </div>
            {hasMetrics && (
                <div className={`order-3 w-full overflow-hidden transition-all duration-300 ease-out md:order-none md:min-w-0 md:flex-1 ${isMetricsExpanded ? 'max-h-[640px] opacity-100' : 'max-h-0 opacity-0 md:max-h-none md:opacity-100'}`}>
                    <div className="flex flex-col gap-2 pt-1 md:pt-0">
                    {hasLiteMetrics && (
                    <div className={metricsRowClass}>
                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">smart_toy</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.model')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className="text-xs font-bold text-white/90 leading-tight">{stock.prediction.modelName}</span>
                            </div>
                        </div>

                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">memory</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.context')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className="text-xs font-bold text-white/90 leading-tight">
                                    {stock.prediction.contextLen
                                        ? (stock.prediction.contextLen < 1024
                                            ? stock.prediction.contextLen
                                            : Math.round(stock.prediction.contextLen / 1024) + 'K')
                                        : '?'}
                                </span>
                            </div>
                        </div>

                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">calendar_today</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.horizon')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className="text-xs font-bold text-white/90 leading-tight">
                                    {stock.prediction.horizonLen || '?'} {t('prediction.days')}
                                </span>
                            </div>
                        </div>

                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">query_stats</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.maxDev')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className="text-xs font-bold text-white/90 leading-tight">
                                    {stock.prediction.maxDeviationPercent?.toFixed(2) ?? '0.00'}%
                                </span>
                            </div>
                        </div>

                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">grade</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.score')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className={`text-xs font-bold leading-tight ${confidenceColor}`}>
                                    {stock.prediction.confidence.toFixed(4)}
                                </span>
                            </div>
                        </div>

                        <div className={liteMetricCardClass}>
                            <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                <span className="material-symbols-outlined text-xs text-white/80">trending_up</span>
                                <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.actChg')}</span>
                            </div>
                            <div className="bg-white/5 px-2 py-1 flex justify-center">
                                <span className="text-xs font-bold leading-tight" style={{ color: actualChangeColor }}>
                                    {actualChangeText}
                                </span>
                            </div>
                        </div>

                        {stock.predictedChangePercent !== undefined && stock.predictedChangePercent !== 0 && (
                            <div className={liteMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={liteHeaderStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">online_prediction</span>
                                    <span className="text-xs font-medium text-white/80 leading-none">{t('prediction.predChg')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold leading-tight text-primary">
                                        {isPredPositive ? '+' : '-'}{Math.abs(stock.predictedChangePercent).toFixed(2)}%
                                    </span>
                                </div>
                            </div>
                        )}
                    </div>
                    )}

                    {hasProMetrics && (
                        <div className={metricsRowClass}>
                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">smart_toy</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.model')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold text-amber-100 leading-tight">{stock.prediction.proModelName || '—'}</span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">memory</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.context')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold text-amber-100 leading-tight">
                                        {formatContextLen(stock.prediction.proContextLen || stock.prediction.contextLen)}
                                    </span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">calendar_today</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.horizon')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold text-amber-100 leading-tight">
                                        {stock.prediction.proHorizonLen || stock.prediction.horizonLen || '?'} {t('prediction.days')}
                                    </span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">query_stats</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.maxDev')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold text-amber-100 leading-tight">
                                        {stock.prediction.proMaxDeviationPercent !== undefined ? `${stock.prediction.proMaxDeviationPercent.toFixed(2)}%` : '—'}
                                    </span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">grade</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.score')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className={`text-xs font-bold leading-tight ${stock.prediction.proConfidence !== undefined ? proConfidenceColor : 'text-amber-100'}`}>
                                        {stock.prediction.proConfidence !== undefined ? stock.prediction.proConfidence.toFixed(4) : '—'}
                                    </span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">trending_up</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.actChg')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className="text-xs font-bold leading-tight" style={{ color: actualChangeColor }}>
                                        {actualChangeText}
                                    </span>
                                </div>
                            </div>

                            <div className={proMetricCardClass}>
                                <div className="px-2 py-1 flex items-center gap-1.5 justify-center" style={proHeaderGradientStyle}>
                                    <span className="material-symbols-outlined text-xs text-white/80">online_prediction</span>
                                    <span className="text-xs font-medium text-white/90 leading-none">{t('prediction.predChg')}</span>
                                </div>
                                <div className="bg-white/5 px-2 py-1 flex justify-center">
                                    <span className={`text-xs font-bold leading-tight ${stock.prediction.proPredictedChangePercent !== undefined ? (isProPredPositive ? 'text-amber-200' : 'text-orange-300') : 'text-amber-100'}`}>
                                        {stock.prediction.proPredictedChangePercent !== undefined
                                            ? `${isProPredPositive ? '+' : '-'}${Math.abs(stock.prediction.proPredictedChangePercent).toFixed(2)}%`
                                            : '—'}
                                    </span>
                                </div>
                            </div>
                        </div>
                    )}
                    </div>
                </div>
            )}
            <div className="flex flex-wrap items-center justify-end gap-2 ml-auto order-2 md:order-none">
                {(watchlistCount !== undefined || onAddToWatchlist) && (
                    <button
                        type="button"
                        onClick={handleWatchlistClick}
                        disabled={!onAddToWatchlist || isAddingToWatchlist}
                        aria-pressed={isWatchlisted}
                        className={`inline-flex h-10 shrink-0 items-center gap-1.5 rounded-full border px-3 shadow-sm transition-colors disabled:cursor-default ${
                            isWatchlisted
                                ? 'border-primary/35 bg-primary/12 text-primary'
                                : 'border-white/10 bg-white/[0.05] text-white/68 hover:border-primary/35 hover:bg-primary/10 hover:text-primary'
                        } ${isAddingToWatchlist ? 'opacity-70' : ''}`}
                        title={`${watchlistTitle} · ${watchlistCountLabel}: ${watchlistCount ?? 0}`}
                        aria-label={`${watchlistTitle}，${watchlistCountLabel}: ${watchlistCount ?? 0}`}
                    >
                        <span
                            className={`material-symbols-outlined text-[18px] ${isAddingToWatchlist ? 'animate-pulse' : ''}`}
                            style={{ fontVariationSettings: `'FILL' ${isWatchlisted ? 1 : 0}` }}
                        >
                            favorite
                        </span>
                        <span className="text-xs font-semibold text-white/62">{watchlistCountLabel}</span>
                        <span className={`text-sm font-black leading-none ${isWatchlisted ? 'text-primary' : 'text-white/88'}`}>
                            {formattedWatchlistCount || '0'}
                        </span>
                    </button>
                )}
                {hasHorizonOptions && (
                    <div
                        className="flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] p-1"
                        aria-label={language === 'zh' ? '预测周期' : 'Prediction period'}
                    >
                        {horizonOptions!.map(option => {
                            const isActive = selectedHorizon === option.value;
                            return (
                                <button
                                    key={option.value}
                                    type="button"
                                    disabled={!option.available}
                                    onClick={() => option.available && onHorizonChange?.(option.value)}
                                    className={`h-8 rounded-md px-2.5 text-[11px] font-bold transition-colors ${
                                        isActive
                                            ? 'bg-primary text-black shadow-sm'
                                            : option.available
                                                ? 'bg-white/[0.06] text-white/70 hover:bg-white/10 hover:text-white'
                                                : 'cursor-not-allowed bg-white/[0.025] text-white/25'
                                    }`}
                                    title={option.available
                                        ? `P${option.value}`
                                        : (language === 'zh' ? `暂无 P${option.value} 数据` : `No P${option.value} data`)}
                                >
                                    P{option.value}
                                </button>
                            );
                        })}
                    </div>
                )}
                {hasMetrics && (
                    <button
                        type="button"
                        onClick={() => setIsMetricsExpanded(prev => !prev)}
                        aria-label={isMetricsExpanded ? t('prediction.hideMetrics') : t('prediction.showMetrics')}
                        title={isMetricsExpanded ? t('prediction.hideMetrics') : t('prediction.showMetrics')}
                        className="flex md:hidden items-center justify-center w-10 h-10 rounded-lg border border-white/10 bg-white/5 text-white/70 transition-colors hover:bg-white/10 hover:text-white"
                    >
                        <span className={`material-symbols-outlined text-[22px] transition-transform duration-300 ${isMetricsExpanded ? 'rotate-180' : ''}`}>
                            expand_more
                        </span>
                    </button>
                )}
            </div>
        </div>
    );
};

const StockPredictionCard: React.FC<StockPredictionCardProps> = ({
    stock,
    onAddToWatchlist,
    className = '',
    chartHeightClassName = 'min-h-[150px] md:min-h-[180px]',
    borderless = false,
    hideSummary = false,
    chartMode,
    onChartModeChange,
    horizonOptions,
    selectedHorizon,
    onHorizonChange,
}) => {
    const { t } = useLanguage();
    const [internalChartMode, setInternalChartMode] = useState<PredictionChartMode>('price');

    const startPrice = stock.currentPrice / (1 + stock.changePercent / 100);
    const effectiveChartMode = chartMode || internalChartMode;
    const handleChartModeChange = onChartModeChange || setInternalChartMode;
    const hasMetrics = Boolean(
        stock.prediction?.modelName
        || stock.prediction?.proModelName
        || stock.prediction?.proConfidence !== undefined
        || stock.prediction?.proMaxDeviationPercent !== undefined
        || stock.prediction?.proPredictedChangePercent !== undefined,
    );

    return (
        <div className={`flex flex-col gap-4 rounded-xl ${borderless ? '' : 'border border-white/10'} bg-card-dark p-6 ${className}`.trim()}>
            {!hideSummary && (
                <StockPredictionSummary
                    stock={stock}
                    onAddToWatchlist={onAddToWatchlist}
                    chartMode={effectiveChartMode}
                    onChartModeChange={handleChartModeChange}
                    horizonOptions={horizonOptions}
                    selectedHorizon={selectedHorizon}
                    onHorizonChange={onHorizonChange}
                />
            )}
            <div className={`flex flex-1 flex-col gap-4 py-4 ${chartHeightClassName}`.trim()}>
                <PredictionChartPanel
                    change={stock.changePercent}
                    chartData={stock.prediction?.chartData}
                    currentPrice={stock.currentPrice}
                    startPrice={startPrice}
                    mode={effectiveChartMode}
                />
            </div>

            {/* Legend Removed */}

            {stock.prediction ? (
                <>
                    {!hasMetrics && (
                        <p className="text-sm text-white/80">{stock.prediction.analysis}</p>
                    )}
                </>
            ) : (
                 <div className="flex flex-col items-center justify-center text-center gap-2">
                    <div className="w-5 h-5 border-2 border-dashed rounded-full animate-spin border-primary"></div>
                    <span className="text-xs text-white/60">{t('prediction.awaitingAI')}</span>
                </div>
            )}
        </div>
    );
};

export default StockPredictionCard;
