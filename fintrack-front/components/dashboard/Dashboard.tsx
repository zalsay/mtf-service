
import React, { useState, useEffect, useCallback } from 'react';
import { StockData } from '../../types';
import StockPredictionCard from './StockPredictionCard';
import AddStockModal from './AddStockModal';
import { useLanguage } from '../../contexts/LanguageContext';
import { watchlistAPI, getPublicPredictions } from '../../services/apiService';
import { mapPredictionResponseToStockData } from '../../utils/predictionUtils';

interface DashboardProps {
    stocks: StockData[];
    isLoading: boolean;
    error: string | null;
    onRefresh?: () => void;
    onAuthError?: () => void;
}

const PREDICTION_HORIZONS = [7, 14, 28] as const;
type PredictionHorizon = typeof PREDICTION_HORIZONS[number];

const FilterChip: React.FC<{ label: string; active?: boolean; onClick: () => void; }> = ({ label, active, onClick }) => (
    <div
        onClick={onClick}
        className={`flex h-8 shrink-0 items-center justify-center gap-x-2 rounded-full px-4 cursor-pointer transition-colors ${active ? 'bg-primary/20 text-primary' : 'bg-white/10 text-white/80 hover:bg-white/20'
            }`}
    >
        <p className="text-sm font-medium leading-normal">{label}</p>
    </div>
);

const SegmentButton: React.FC<{
    label: string;
    active: boolean;
    onClick: () => void;
}> = ({ label, active, onClick }) => (
    <button
        onClick={onClick}
        className={`px-6 py-2 rounded-md text-sm font-bold transition-all ${active ? 'bg-primary text-black shadow-sm' : 'text-white/60 hover:text-white hover:bg-white/5'}`}
    >
        {label}
    </button>
);

const normalizeDashboardSymbol = (symbol: string): string => {
    const lower = String(symbol || '').trim().toLowerCase();
    const digits = lower.replace(/[^0-9]/g, '');
    return digits || lower;
};

const Dashboard: React.FC<DashboardProps> = ({ stocks: propStocks, isLoading: propIsLoading, error: propError, onRefresh, onAuthError }) => {
    const { t, language } = useLanguage();
    const [activeFilter, setActiveFilter] = useState('All');
    const [activeAssetType, setActiveAssetType] = useState<1 | 2>(1);
    const [selectedHorizonBySymbol, setSelectedHorizonBySymbol] = useState<Record<string, PredictionHorizon>>({});
    const [isModalOpen, setIsModalOpen] = useState(false);
    const filters = ['All', 'Highest Confidence', 'Potential Growth', 'Bullish', 'Bearish'];
    
    const [publicStocks, setPublicStocks] = useState<StockData[]>([]);
    const [watchlistSymbols, setWatchlistSymbols] = useState<Set<string>>(new Set());
    const [isFetching, setIsFetching] = useState(true);
    const [fetchError, setFetchError] = useState<string | null>(null);

    const applyWatchlistState = (stocks: StockData[], symbols: Set<string>) => (
        stocks.map(stock => ({
            ...stock,
            isWatchlisted: symbols.has(normalizeDashboardSymbol(stock.symbol)),
        }))
    );

    const fetchPublic = useCallback(async (options?: { reset?: boolean }) => {
        if (options?.reset) {
            setPublicStocks([]);
        }
        setIsFetching(true);
        setFetchError(null);
        try {
            const [predictionResult, watchlistResult] = await Promise.allSettled([
                getPublicPredictions(),
                watchlistAPI.getWatchlist(),
            ]);
            let nextWatchlistSymbols = new Set<string>();
            if (watchlistResult.status === 'fulfilled') {
                nextWatchlistSymbols = new Set(
                    (watchlistResult.value.watchlist || [])
                        .map(item => normalizeDashboardSymbol(item.stock?.symbol || ''))
                        .filter(Boolean),
                );
                setWatchlistSymbols(nextWatchlistSymbols);
            } else {
                const message = watchlistResult.reason?.message || '';
                if (onAuthError && (
                    message.includes('Authorization header required')
                    || message.includes('401')
                    || message.includes('Unauthorized')
                )) {
                    onAuthError();
                }
            }

            if (predictionResult.status === 'fulfilled' && predictionResult.value?.items) {
                const mapped = mapPredictionResponseToStockData(predictionResult.value, language);
                setPublicStocks(applyWatchlistState(
                    mapped.filter(stock => Boolean(stock.prediction?.horizonLen)),
                    nextWatchlistSymbols,
                ));
            } else {
                setPublicStocks([]);
                if (predictionResult.status === 'rejected') {
                    throw predictionResult.reason;
                }
            }
        } catch (e: any) {
            setFetchError(e.message || "Failed to load public predictions");
        } finally {
            setIsFetching(false);
        }
    }, [language, onAuthError]);

    useEffect(() => {
        void fetchPublic({ reset: true });
    }, [fetchPublic]);

    const handleAddStock = async (symbol: string, type: 1 | 2 = 1) => {
        const normalizedSymbol = normalizeDashboardSymbol(symbol);
        if (watchlistSymbols.has(normalizedSymbol)) {
            return;
        }

        try {
            await watchlistAPI.addToWatchlist({ symbol, stock_type: type });
        } catch (err: any) {
            const message = err?.message || '';
            if (message.includes('duplicate symbol')) {
                return;
            }
            throw err;
        }

        setWatchlistSymbols(current => {
            const next = new Set(current);
            next.add(normalizedSymbol);
            return next;
        });
        setPublicStocks(current => current.map(stock => {
            if (normalizeDashboardSymbol(stock.symbol) !== normalizedSymbol) {
                return stock;
            }
            return {
                ...stock,
                isWatchlisted: true,
                watchlistCount: (stock.watchlistCount || 0) + 1,
            };
        }));
        if (onRefresh) {
            onRefresh();
        }
    };

    void propStocks;
    void propIsLoading;

    const displayStocks = publicStocks;
    const isLoading = isFetching;
    const error = propError || fetchError;

    const pickDefaultHorizon = (horizons: PredictionHorizon[]): PredictionHorizon => (
        horizons.includes(7) ? 7 : (horizons[0] || 7)
    );
    const groupedStocks = Array.from(displayStocks.reduce((groups, stock) => {
        const horizonLen = stock.prediction?.horizonLen;
        if (!horizonLen || !PREDICTION_HORIZONS.includes(horizonLen as PredictionHorizon)) {
            return groups;
        }
        const key = stock.symbol;
        const list = groups.get(key) || [];
        list.push(stock);
        groups.set(key, list);
        return groups;
    }, new Map<string, StockData[]>()).entries()).map(([symbol, items]) => {
        const sortedItems = [...items].sort((left, right) => (
            PREDICTION_HORIZONS.indexOf((left.prediction?.horizonLen || 7) as PredictionHorizon)
            - PREDICTION_HORIZONS.indexOf((right.prediction?.horizonLen || 7) as PredictionHorizon)
        ));
        const availableHorizons = Array.from(new Set(sortedItems
            .map(item => item.prediction?.horizonLen)
            .filter((value): value is PredictionHorizon => PREDICTION_HORIZONS.includes(value as PredictionHorizon))));
        const preferredHorizon = selectedHorizonBySymbol[symbol];
        const activeHorizon = preferredHorizon && availableHorizons.includes(preferredHorizon)
            ? preferredHorizon
            : pickDefaultHorizon(availableHorizons);
        const activeStock = sortedItems.find(item => item.prediction?.horizonLen === activeHorizon) || sortedItems[0];

        return {
            symbol,
            stock: activeStock,
            activeHorizon,
            availableHorizons,
        };
    });

    const filteredStocks = groupedStocks.filter(group => {
        const stock = group.stock;
        if ((stock.stockType || 1) !== activeAssetType) {
            return false;
        }
        if (!stock.prediction) return activeFilter === 'All';
        switch (activeFilter) {
            case 'Highest Confidence':
                return stock.prediction.confidence > 85;
            case 'Potential Growth':
                return (stock.prediction.predicted_high / stock.currentPrice - 1) * 100 > 5;
            case 'Bullish':
                return stock.prediction.sentiment === 'Bullish';
            case 'Bearish':
                return stock.prediction.sentiment === 'Bearish';
            default:
                return true;
        }
    }).sort((left, right) => {
        const countDelta = (right.stock.watchlistCount || 0) - (left.stock.watchlistCount || 0);
        if (countDelta !== 0) {
            return countDelta;
        }
        return left.symbol.localeCompare(right.symbol);
    });

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-wrap justify-between gap-4 items-center">
                <div className="flex w-full flex-col gap-1">
                    <div className="flex items-start justify-between gap-3 lg:block">
                        <h1 className="text-white text-4xl font-black leading-tight tracking-[-0.033em]">{t('dashboard.title')}</h1>
                        <div className="flex items-center gap-2 lg:hidden">
                            <button
                                type="button"
                                onClick={() => void fetchPublic()}
                                disabled={isFetching}
                                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-white/5 text-white/75 transition-colors hover:bg-white/10 hover:text-white disabled:cursor-not-allowed disabled:opacity-60"
                                title={t('uzi.refresh', language === 'zh' ? '刷新' : 'Refresh')}
                            >
                                <span className={`material-symbols-outlined text-[20px] ${isFetching ? 'animate-spin' : ''}`}>refresh</span>
                            </button>
                            <button
                                onClick={() => setIsModalOpen(true)}
                                className="flex h-10 shrink-0 items-center justify-center gap-2 rounded-lg bg-primary px-3 text-sm font-bold leading-normal tracking-[0.015em] text-background-dark transition-opacity hover:opacity-90"
                            >
                                <span className="material-symbols-outlined text-[20px]" style={{ fontVariationSettings: "'FILL' 1" }}>add</span>
                                <span className="truncate">{t('dashboard.addStock')}</span>
                            </button>
                        </div>
                    </div>
                    <p className="text-white/60 text-base font-normal leading-normal">{t('dashboard.subtitle')}</p>
                </div>
            </header>
            <div className="flex flex-col gap-4">
                <div className="flex flex-wrap justify-between gap-2 items-center">
                    <div className="flex flex-wrap gap-2 items-center">
                        <div className="flex space-x-1 bg-white/5 rounded-lg p-1">
                            <SegmentButton
                                label={t('watchlist.tabStock')}
                                active={activeAssetType === 1}
                                onClick={() => setActiveAssetType(1)}
                            />
                            <SegmentButton
                                label={t('watchlist.tabEtf')}
                                active={activeAssetType === 2}
                                onClick={() => setActiveAssetType(2)}
                            />
                        </div>
                    </div>
                    <div className="hidden lg:flex items-center gap-2">
                        <button
                            type="button"
                            onClick={() => void fetchPublic()}
                            disabled={isFetching}
                            className="flex items-center justify-center gap-2 px-4 h-10 rounded-lg border border-white/10 bg-white/5 text-white text-sm font-bold leading-normal tracking-[0.015em] transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                            <span className={`material-symbols-outlined ${isFetching ? 'animate-spin' : ''}`}>refresh</span>
                            <span className="truncate">{t('uzi.refresh', language === 'zh' ? '刷新' : 'Refresh')}</span>
                        </button>
                        <button
                            onClick={() => setIsModalOpen(true)}
                            className="flex items-center justify-center gap-2 px-4 h-10 rounded-lg bg-primary text-background-dark text-sm font-bold leading-normal tracking-[0.015em] hover:opacity-90 transition-opacity"
                        >
                            <span className="material-symbols-outlined" style={{ fontVariationSettings: "'FILL' 1" }}>add</span>
                            <span className="truncate">{t('dashboard.addStock')}</span>
                        </button>
                    </div>
                </div>
                <div className="flex gap-3 overflow-x-auto pb-2 -mx-1 px-1 hidden">
                    {filters.map(filter => (
                        <FilterChip
                            key={filter}
                            label={t(`dashboard.filters.${filter.replace(/\s+/g, '').charAt(0).toLowerCase() + filter.replace(/\s+/g, '').slice(1)}`)}
                            active={activeFilter === filter}
                            onClick={() => setActiveFilter(filter)}
                        />
                    ))}
                </div>
            </div>

            {error && (
                <div className="bg-red-900/50 border border-red-500/50 text-red-300 p-4 rounded-lg text-center">
                    <p className="font-bold">{t('dashboard.errorTitle')}</p>
                    <p className="text-sm">{error}</p>
                </div>
            )}

            <div className="grid grid-cols-1 gap-6">
                {isLoading ? (
                    Array.from({ length: 6 }).map((_, index) => (
                        <div key={index} className="flex flex-col gap-4 rounded-xl border border-white/10 bg-card-dark p-6 min-h-[350px] animate-pulse">
                            <div className="flex justify-between items-start">
                                <div>
                                    <div className="h-6 w-16 bg-white/10 rounded"></div>
                                    <div className="h-4 w-24 bg-white/10 rounded mt-2"></div>
                                </div>
                                <div>
                                    <div className="h-8 w-20 bg-white/10 rounded"></div>
                                    <div className="h-4 w-12 bg-white/10 rounded mt-2 ml-auto"></div>
                                </div>
                            </div>
                            <div className="flex-1 bg-white/5 rounded-lg"></div>
                            <div className="h-4 w-full bg-white/10 rounded"></div>
                            <div className="flex justify-between items-center">
                                <div className="h-4 w-1/3 bg-white/10 rounded"></div>
                                <div className="h-4 w-1/4 bg-white/10 rounded"></div>
                            </div>
                        </div>
                    ))
                ) : (
                    filteredStocks.map(group => (
                        <StockPredictionCard
                            key={group.symbol}
                            stock={group.stock}
                            onAddToWatchlist={handleAddStock}
                            horizonOptions={PREDICTION_HORIZONS.map(value => ({
                                value,
                                available: group.availableHorizons.includes(value),
                            }))}
                            selectedHorizon={group.activeHorizon}
                            onHorizonChange={value => {
                                setSelectedHorizonBySymbol(current => ({
                                    ...current,
                                    [group.symbol]: value as PredictionHorizon,
                                }));
                            }}
                        />
                    ))
                )}
            </div>
            {!isLoading && filteredStocks.length === 0 && (
                <div className="text-center col-span-full py-12 bg-card-dark rounded-xl">
                    <p className="text-white/80">{t('dashboard.noStocks')}</p>
                </div>
            )}

            <AddStockModal
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                onAdd={handleAddStock}
            />
        </div>
    );
};

export default Dashboard;
