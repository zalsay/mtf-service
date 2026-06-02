import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import {
    authAPI,
    normalizeUZITicker,
    quotesAPI,
    settingsAPI,
    UZIAnalyzeStatus,
    UZIReportItem,
    uziAPI,
    watchlistAPI,
    WatchlistItem,
} from '../../services/apiService';
import { getChangeColors } from '../../utils/colorUtils';
import {
    formatUpdatedAt,
    getDepthMeta,
    getLatestReportByDepth,
    getTodayReport,
    getUserFacingError,
    isUZIBackgroundProcessingError,
    isUZIAuthError,
    UZIDepth,
} from './uziUtils';

interface UZIReportsProps {
    onAuthError?: () => void;
}

const standardReportStatusStyles = {
    ready: 'border-primary bg-primary text-black shadow-sm',
    empty: 'border-white/10 bg-white/[0.04] text-white/40',
};

const STANDARD_REPORT_DEPTH: UZIDepth = 'medium';
const UZI_REPORT_JOB_POLL_INTERVAL_MS = 2000;

type ReportJobState = {
    jobID: string;
    status: 'queued' | 'running' | 'completed' | 'failed';
    error?: string;
    report?: UZIReportItem;
};

const isActiveReportJob = (state?: ReportJobState) => (
    state?.status === 'queued' || state?.status === 'running'
);

const UZIReports: React.FC<UZIReportsProps> = ({ onAuthError }) => {
    const { t, language } = useLanguage();
    const isZh = language === 'zh';
    const copy = {
        title: isZh ? '研报 Skill' : 'Research Skill',
        ready: isZh ? '可生成' : 'Ready',
        tryLater: isZh ? '稍后再试' : 'Try Later',
        configureFirst: isZh ? '先配置模型' : 'Configure Model',
        subtitle: isZh
            ? '· 17 种机构分析方法 · 51 评委分析 · 180个使用规则，生成研报'
            : '17 institutional analysis methods, 51 review agents, and 180 usage rules for report generation',
        configureAIModel: isZh ? '请先在设置中配置 AI 模型，再生成研报。' : 'Configure the AI model in Settings before generating reports.',
        adminRule: isZh ? '管理员账号可重复生成同一股票标准研报。' : 'Admin accounts can regenerate standard reports for the same ticker.',
        dailyRule: isZh ? '每只股票每天只能生成一次标准研报。' : 'Each ticker can generate one standard report per day.',
        searchPlaceholder: isZh ? '搜索股票' : 'Search stocks',
        refresh: isZh ? '刷新' : 'Refresh',
        openReport: isZh ? '打开报告' : 'Open Report',
        stockCount: isZh ? '只股票' : 'stocks',
        reportCount: isZh ? '份研报' : 'reports',
        open: isZh ? '打开' : 'Open',
        generated: isZh ? '已生成' : 'Generated',
        notGenerated: isZh ? '未生成' : 'Not Generated',
        generatedAt: isZh ? '生成时间' : 'Generated At',
        report: isZh ? '研报' : 'Report',
        generateStandard: isZh ? '生成标准研报' : 'Generate Standard Report',
        generating: isZh ? '生成中' : 'Generating',
        addWatchFirstTitle: isZh ? '先添加关注，再生成研报' : 'Add watchlist items before generating reports',
        addWatchFirstDesc: isZh ? '研报入口仅对我的关注开放。' : 'Report generation is available for your watchlist only.',
        queued: isZh ? '排队中' : 'Queued',
        processing: isZh ? '处理中·稍后刷新页面查收研报。' : 'Processing. Refresh later to check the report.',
        completed: isZh ? '已完成' : 'Completed',
        failed: isZh ? '失败' : 'Failed',
        noAIModelError: isZh ? '请先在设置中配置 AI 模型后再生成研报' : 'Configure the AI model in Settings before generating reports.',
        completedSummary: isZh ? '研报生成完成，可直接打开查看' : 'Report generation completed. You can open it now.',
        failedSummary: isZh ? '研报生成失败' : 'Report generation failed',
        statusQueryFailed: isZh ? '研报状态查询失败' : 'Failed to query report status',
    };
    const [watchlistItems, setWatchlistItems] = useState<WatchlistItem[]>([]);
    const [reportsByTicker, setReportsByTicker] = useState<Record<string, UZIReportItem[]>>({});
    const [latestQuotes, setLatestQuotes] = useState<Record<string, { latest_price?: number; change_percent?: number; trading_date?: string; turnover_rate?: number }>>({});
    const [activeTab, setActiveTab] = useState<1 | 2>(1);
    const [searchTerm, setSearchTerm] = useState('');
    const [isLoading, setIsLoading] = useState(false);
    const [generatingSymbol, setGeneratingSymbol] = useState<string | null>(null);
    const [reportJobsByTicker, setReportJobsByTicker] = useState<Record<string, ReportJobState>>({});
    const [serviceReady, setServiceReady] = useState(true);
    const [aiModelReady, setAIModelReady] = useState(false);
    const [isAdminUser, setIsAdminUser] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [streamState, setStreamState] = useState<{
        ticker: string | null;
        status: 'idle' | 'running' | 'processing' | 'completed' | 'failed';
        stage?: string;
        summary?: string;
        reportPath?: string;
    }>({
        ticker: null,
        status: 'idle',
    });
    const streamStatusRef = useRef<typeof streamState.status>('idle');
    const lastRemoteCompletedRef = useRef<string>('');

    useEffect(() => {
        streamStatusRef.current = streamState.status;
    }, [streamState.status]);

    const setPageErrorFromMessage = (message?: string | null) => {
        const userFacingError = getUserFacingError(message, language);
        if ((userFacingError === '当前请求未完成，请稍后再试。' || userFacingError === 'The request is still pending. Please try again later.')
            && streamStatusRef.current === 'completed') {
            return;
        }
        setError(userFacingError);
    };

    const handleAPIError = (message: string) => {
        if (onAuthError && isUZIAuthError(message)) {
            onAuthError();
            return true;
        }
        return false;
    };

    const loadReports = async () => {
        const response = await uziAPI.listReports();
        const nextMap: Record<string, UZIReportItem[]> = {};
        for (const report of response.items || []) {
            const reportTicker = normalizeUZITicker(report.ticker);
            if (!nextMap[reportTicker]) {
                nextMap[reportTicker] = [];
            }
            nextMap[reportTicker].push(report);
        }
        setReportsByTicker(nextMap);
    };

    const loadAIModelConfig = async () => {
        const config = await settingsAPI.getAIModelConfig();
        setAIModelReady(Boolean(config.base_url && config.model_id && config.has_api_key));
    };

    const loadUserProfile = async () => {
        const profile = await authAPI.getProfile();
        setIsAdminUser(Boolean(profile?.is_admin));
    };

    const loadWatchlist = async () => {
        const response = await watchlistAPI.getWatchlist();
        const nextItems = (response.watchlist || []).filter((item): item is WatchlistItem => Boolean(item?.stock?.symbol));
        setWatchlistItems(nextItems);

        const symbols = nextItems.map(item => item.stock.symbol).filter(Boolean);
        if (symbols.length === 0) {
            setLatestQuotes({});
            return;
        }

        try {
            const quoteResponse = await quotesAPI.batchLatest(symbols);
            const nextQuotes: Record<string, { latest_price?: number; change_percent?: number; trading_date?: string; turnover_rate?: number }> = {};
            for (const quote of quoteResponse.quotes || []) {
                nextQuotes[quote.symbol] = {
                    latest_price: quote.latest_price,
                    change_percent: quote.change_percent,
                    trading_date: quote.trading_date,
                    turnover_rate: quote.turnover_rate,
                };
            }
            setLatestQuotes(nextQuotes);
        } catch {
            setLatestQuotes({});
        }
    };

    const loadPage = async () => {
        try {
            setIsLoading(true);
            setError(null);
            const [, , , , remoteStatus] = await Promise.all([
                loadWatchlist(),
                loadReports(),
                loadAIModelConfig(),
                loadUserProfile(),
                uziAPI.getAnalyzeStatus().catch(() => null),
            ]);
            if (remoteStatus) {
                applyRemoteAnalyzeStatus(remoteStatus);
            }
            try {
                await uziAPI.health();
                setServiceReady(true);
            } catch {
                setServiceReady(false);
            }
        } catch (err: any) {
            const message = err?.message || 'Failed to load research skill';
            if (!handleAPIError(message)) {
                setPageErrorFromMessage(message);
            }
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => {
        void loadPage();
    }, []);

    useEffect(() => {
        const socket = uziAPI.connectAnalyzeStatus(status => {
            applyRemoteAnalyzeStatus(status);
        });
        return () => {
            socket?.close();
        };
    }, []);

    useEffect(() => {
        const activeJobs = Object.entries(reportJobsByTicker).filter(([, job]) => isActiveReportJob(job));
        if (activeJobs.length === 0) {
            return;
        }

        let stopped = false;
        const pollJobs = async () => {
            let shouldReloadReports = false;
            await Promise.all(activeJobs.map(async ([ticker, job]) => {
                try {
                    const status = await uziAPI.getAnalyzeJobStatus(job.jobID);
                    if (stopped) {
                        return;
                    }
                    if (status.status === 'queued' || status.status === 'pending') {
                        setReportJobsByTicker(prev => ({
                            ...prev,
                            [ticker]: { ...(prev[ticker] || { jobID: job.jobID, status: 'queued' as const }), jobID: job.jobID, status: 'queued' },
                        }));
                        return;
                    }
                    if (status.status === 'running') {
                        setReportJobsByTicker(prev => ({
                            ...prev,
                            [ticker]: { ...(prev[ticker] || { jobID: job.jobID, status: 'running' as const }), jobID: job.jobID, status: 'running' },
                        }));
                        return;
                    }
                    if (status.status === 'succeeded') {
                        shouldReloadReports = true;
                        const report = status.report || status.result?.report;
                        setReportJobsByTicker(prev => ({
                            ...prev,
                            [ticker]: { jobID: job.jobID, status: 'completed', report },
                        }));
                        setGeneratingSymbol(prev => (prev === ticker ? null : prev));
                        setStreamState({
                            ticker,
                            status: 'completed',
                            stage: 'finalizing',
                            summary: copy.completedSummary,
                            reportPath: report?.report_relative_path || status.result?.report_relative_path,
                        });
                        return;
                    }
                    setReportJobsByTicker(prev => ({
                        ...prev,
                        [ticker]: { jobID: job.jobID, status: 'failed', error: status.error || copy.failedSummary },
                    }));
                    setGeneratingSymbol(prev => (prev === ticker ? null : prev));
                    setStreamState(prev => ({
                        ...prev,
                        ticker,
                        status: 'failed',
                        summary: status.error || copy.failedSummary,
                    }));
                } catch (err: any) {
                    if (stopped) {
                        return;
                    }
                    const message = err?.message || copy.statusQueryFailed;
                    setReportJobsByTicker(prev => ({
                        ...prev,
                        [ticker]: { jobID: job.jobID, status: 'failed', error: getUserFacingError(message, language) },
                    }));
                    setGeneratingSymbol(prev => (prev === ticker ? null : prev));
                }
            }));
            if (shouldReloadReports && !stopped) {
                await loadReports();
            }
        };

        const timer = window.setInterval(() => {
            void pollJobs();
        }, UZI_REPORT_JOB_POLL_INTERVAL_MS);
        return () => {
            stopped = true;
            window.clearInterval(timer);
        };
    }, [reportJobsByTicker, language]);

    const filteredItems = useMemo(() => {
        const keyword = searchTerm.trim().toLowerCase();
        return watchlistItems.filter(item => {
            const symbol = item.stock?.symbol || '';
            const companyName = item.stock?.company_name || '';
            const itemType = item.stock_type || 1;
            const matchesSearch = !keyword
                || symbol.toLowerCase().includes(keyword)
                || companyName.toLowerCase().includes(keyword);
            return matchesSearch && itemType === activeTab;
        });
    }, [activeTab, searchTerm, watchlistItems]);

    const totalReports = Object.values(reportsByTicker).reduce((sum, items) => (
        sum + items.filter(report => report.depth !== 'deep').length
    ), 0);

    const handleGenerate = async (item: WatchlistItem) => {
        const ticker = normalizeUZITicker(item.stock?.symbol || '');
        if (!ticker) {
            return;
        }
        const todayReport = getTodayReport(reportsByTicker[ticker], STANDARD_REPORT_DEPTH);
        if (todayReport && !isAdminUser) {
            const depthLabel = getDepthMeta(STANDARD_REPORT_DEPTH, language).label;
            setError(null);
            setStreamState({
                ticker,
                status: 'completed',
                stage: 'daily-limit',
                summary: isZh
                    ? `今日已生成该股票${depthLabel}研报，可直接打开今日研报。`
                    : `A ${depthLabel.toLowerCase()} report has already been generated for this ticker today. You can open today's report.`,
                reportPath: todayReport.report_relative_path,
            });
            return;
        }
        if (!aiModelReady) {
            setError(copy.noAIModelError);
            return;
        }

        try {
            setGeneratingSymbol(ticker);
            setError(null);
            setStreamState({
                ticker,
                status: 'running',
                stage: 'queued',
                summary: copy.queued,
            });
            const accepted = await uziAPI.enqueueAnalyze({
                ticker,
                depth: STANDARD_REPORT_DEPTH,
                no_resume: false,
            });
            const acceptedStatus = accepted.status === 'running' ? 'running' : 'queued';
            setReportJobsByTicker(prev => ({
                ...prev,
                [ticker]: {
                    jobID: accepted.job_id,
                    status: acceptedStatus,
                },
            }));
            setStreamState(prev => ({
                ...prev,
                ticker: accepted.ticker || ticker,
                status: 'running',
                stage: acceptedStatus,
                summary: acceptedStatus === 'queued' ? copy.queued : copy.generating,
            }));
            setError(null);
        } catch (err: any) {
            const message = err?.message || 'Failed to generate report';
            if (!handleAPIError(message)) {
                if (isUZIBackgroundProcessingError(message)) {
                    setError(null);
                    setStreamState(prev => ({
                        ...prev,
                        ticker,
                        status: 'processing',
                        stage: 'background',
                        summary: copy.processing,
                    }));
                    window.setTimeout(() => {
                        void loadReports();
                    }, 15000);
                } else {
                    setStreamState(prev => ({
                        ...prev,
                        ticker,
                        status: 'failed',
                        summary: getUserFacingError(message, language),
                    }));
                }
            }
            setGeneratingSymbol(null);
        }
    };

        const applyRemoteAnalyzeStatus = (status: UZIAnalyzeStatus) => {
        if (!status || status.status === 'idle') {
            return;
        }

        const ticker = normalizeUZITicker(status.ticker || '');
        const jobID = status.job_id || '';
        if (status.status === 'queued') {
            if (ticker && jobID) {
                setGeneratingSymbol(ticker);
                setReportJobsByTicker(prev => ({
                    ...prev,
                    [ticker]: { jobID, status: 'queued' },
                }));
            } else {
                setGeneratingSymbol(null);
                return;
            }
            setStreamState(prev => ({
                ...prev,
                ticker: ticker || prev.ticker,
                status: 'running',
                stage: 'queued',
                summary: copy.queued,
                reportPath: status.report?.report_relative_path || prev.reportPath,
            }));
            return;
        }

        if (status.status === 'running' || status.status === 'processing') {
            if (ticker && jobID) {
                setGeneratingSymbol(ticker);
                setReportJobsByTicker(prev => ({
                    ...prev,
                    [ticker]: {
                        jobID,
                        status: status.status === 'processing' ? 'running' : 'running',
                    },
                }));
            } else {
                setGeneratingSymbol(null);
                return;
            }
            setStreamState(prev => ({
                ...prev,
                ticker: ticker || prev.ticker,
                status: status.status === 'processing' ? 'processing' : 'running',
                stage: status.stage || prev.stage,
                summary: status.status === 'processing' ? copy.processing : copy.generating,
                reportPath: status.report?.report_relative_path || prev.reportPath,
            }));
            return;
        }

        if (status.status === 'completed') {
            setGeneratingSymbol(null);
            if (ticker && jobID) {
                setReportJobsByTicker(prev => ({
                    ...prev,
                    [ticker]: { jobID, status: 'completed', report: status.report },
                }));
            }
            setStreamState({
                ticker,
                status: 'completed',
                stage: status.stage,
                summary: status.summary || copy.completedSummary,
                reportPath: status.report?.report_relative_path,
            });
            const completedKey = `${ticker}:${status.updated_at || status.report?.report_relative_path || ''}`;
            if (completedKey && completedKey !== lastRemoteCompletedRef.current) {
                lastRemoteCompletedRef.current = completedKey;
                void loadReports();
            }
            return;
        }

        if (status.status === 'failed') {
            setGeneratingSymbol(null);
            if (ticker && jobID) {
                setReportJobsByTicker(prev => ({
                    ...prev,
                    [ticker]: { jobID, status: 'failed', error: status.summary || copy.failedSummary },
                }));
            }
            setStreamState({
                ticker,
                status: 'failed',
                stage: status.stage,
                summary: status.summary || copy.failedSummary,
            });
        }
    };

    const getStreamStatusText = () => {
        if (streamState.status === 'running') {
            return copy.generating;
        }
        if (streamState.status === 'processing') {
            return copy.processing;
        }
        if (streamState.status === 'completed') {
            return copy.completed;
        }
        if (streamState.status === 'failed') {
            return copy.failed;
        }
        return '';
    };

    const getReportJobStatusText = (state?: ReportJobState) => {
        if (state?.status === 'queued') {
            return copy.queued;
        }
        if (state?.status === 'running') {
            return copy.generating;
        }
        if (state?.status === 'completed') {
            return copy.completed;
        }
        if (state?.status === 'failed') {
            return copy.failed;
        }
        return '';
    };

    const getReportJobStatusClass = (state?: ReportJobState) => {
        if (state?.status === 'completed') {
            return 'border-primary bg-primary text-black shadow-sm';
        }
        if (state?.status === 'failed') {
            return 'border-red-400/25 bg-red-500/10 text-red-200';
        }
        return 'border-primary/25 bg-primary/10 text-primary';
    };

    const streamSummaryText = streamState.status === 'running' || streamState.status === 'processing'
        ? ''
        : streamState.summary;

    const handleOpen = async (report?: UZIReportItem) => {
        if (!report?.report_relative_path) {
            return;
        }
        try {
            setError(null);
            await uziAPI.openReport(report.report_relative_path);
        } catch (err: any) {
            const message = err?.message || 'Failed to open report';
            if (!handleAPIError(message)) {
                setPageErrorFromMessage(message);
            }
        }
    };

    const handleOpenPath = async (reportPath?: string) => {
        if (!reportPath) {
            return;
        }
        await handleOpen({ report_relative_path: reportPath } as UZIReportItem);
    };

        return (
        <div className="flex w-full flex-col">
            <div className="mb-4 flex flex-wrap justify-between gap-3 p-4">
                <div className="flex min-w-72 flex-col gap-2">
                    <div className="flex items-center gap-3">
                        <p className="text-white text-4xl font-black leading-tight tracking-[-0.033em]">{copy.title}</p>
                        <span className={`rounded-full px-3 py-1 text-xs font-semibold ${serviceReady && aiModelReady ? 'bg-primary/15 text-primary' : 'bg-white/10 text-white/55'}`}>
                            {serviceReady && aiModelReady ? copy.ready : aiModelReady ? copy.tryLater : copy.configureFirst}
                        </span>
                    </div>
                    <p className="text-white/60 text-base font-normal leading-normal">{copy.subtitle}</p>
                </div>
            </div>

            {error ? (
                <div className="mx-4 mb-3 rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-300">
                    {error}
                </div>
            ) : null}

            {!aiModelReady && !isLoading ? (
                <div className="mx-4 mb-3 flex items-center gap-2 rounded-xl border border-amber-300/20 bg-amber-300/10 px-4 py-3 text-sm font-medium text-amber-100">
                    <span className="material-symbols-outlined text-[18px]">settings</span>
                    <span>{copy.configureAIModel}</span>
                </div>
            ) : null}

                <div className="mx-4 mb-3 flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-4 py-3 text-sm font-medium text-white/65">
                    <span className="material-symbols-outlined text-[18px] text-primary">info</span>
                    <span>{isAdminUser ? copy.adminRule : copy.dailyRule}</span>
                </div>

            <div className="mb-4 px-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                        <div className="flex flex-wrap items-center gap-3">
                            <div className="flex space-x-1 rounded-lg bg-white/5 p-1 w-fit">
                            <button
                                className={`px-6 py-2 rounded-md text-sm font-bold transition-all ${activeTab === 1 ? 'bg-primary text-black shadow-sm' : 'text-white/60 hover:text-white hover:bg-white/5'}`}
                                onClick={() => setActiveTab(1)}
                            >
                                {t('watchlist.tabStock')}
                            </button>
                            <button
                                className={`px-6 py-2 rounded-md text-sm font-bold transition-all ${activeTab === 2 ? 'bg-primary text-black shadow-sm' : 'text-white/60 hover:text-white hover:bg-white/5'}`}
                                onClick={() => setActiveTab(2)}
                            >
                                {t('watchlist.tabEtf')}
                                </button>
                            </div>
                        </div>

                    <div className="flex w-full flex-col gap-3 sm:flex-row lg:w-auto lg:justify-end">
                        <div className="flex h-12 flex-1 items-stretch rounded-lg border border-white/10 bg-white/5 transition-all duration-200 focus-within:border-primary focus-within:ring-2 focus-within:ring-primary/50 sm:min-w-[280px] lg:flex-none">
                            <div className="flex items-center justify-center pl-4 text-white/60">
                                <span className="material-symbols-outlined">search</span>
                            </div>
                            <input
                                className="form-input flex h-full w-full min-w-0 flex-1 resize-none overflow-hidden rounded-lg border-none bg-transparent px-4 pl-2 text-base font-normal leading-normal text-white placeholder:text-white/40 focus:outline-0 focus:ring-0"
                                placeholder={copy.searchPlaceholder}
                                value={searchTerm}
                                onChange={event => setSearchTerm(event.target.value)}
                            />
                        </div>

                        <button
                            className="flex h-12 w-full items-center justify-center gap-2 rounded-lg border border-white/10 bg-white/5 px-5 text-sm font-bold text-white transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
                            onClick={() => void loadPage()}
                            disabled={isLoading || Boolean(generatingSymbol)}
                        >
                            <span className="material-symbols-outlined">refresh</span>
                            <span className="truncate">{copy.refresh}</span>
                        </button>
                    </div>
                </div>
            </div>

            {streamState.status !== 'idle' ? (
                <div className={`mx-4 mb-3 rounded-xl border px-4 py-3 ${
                    streamState.status === 'failed'
                        ? 'border-red-500/20 bg-red-500/10'
                        : 'border-primary/20 bg-primary/10'
                }`}>
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                        <div>
                            <p className={`text-sm font-semibold ${
                                streamState.status === 'failed' ? 'text-red-300' : 'text-primary'
                            }`}>
                                {streamState.ticker || 'UZI'} · {getStreamStatusText()}
                            </p>
                            {streamSummaryText ? (
                                <p className="mt-1 text-sm text-white/80">{streamSummaryText}</p>
                            ) : null}
                        </div>
                        <div className="flex items-center gap-3">
                            {streamState.status === 'completed' && streamState.reportPath ? (
                                <button
                                    type="button"
                                    onClick={() => void handleOpenPath(streamState.reportPath)}
                                    className="inline-flex h-9 items-center justify-center rounded-lg border border-primary/30 bg-primary px-4 text-sm font-semibold text-black transition-colors hover:bg-primary/90"
                                >
                                    {copy.openReport}
                                </button>
                            ) : null}
                        </div>
                    </div>
                </div>
            ) : null}

            {watchlistItems.length > 0 ? (
                <div className="px-4 py-2 @container">
                    <div className="mb-3 flex items-center justify-between px-1 text-xs text-white/45">
                        <span>{filteredItems.length} {copy.stockCount}</span>
                        <span>{totalReports} {copy.reportCount}</span>
                    </div>
                    <div className="space-y-3 md:hidden">
                        {filteredItems.map(item => {
                            const symbol = item.stock.symbol;
                            const uziTicker = normalizeUZITicker(symbol);
                            const companyName = item.stock.company_name;
                            const changePercent = latestQuotes[symbol]?.change_percent ?? item.current_price?.change_percent ?? 0;
                            const isPositive = changePercent >= 0;
                                const { textClass } = getChangeColors(isPositive, language);
                                const currentPrice = latestQuotes[symbol]?.latest_price ?? item.current_price?.price;
                                const rowJob = reportJobsByTicker[uziTicker];
                                const isGeneratingRow = isActiveReportJob(rowJob) || generatingSymbol === uziTicker;
                                const actionStatusText = getReportJobStatusText(rowJob);
                                const standardReport = getLatestReportByDepth(reportsByTicker[uziTicker], STANDARD_REPORT_DEPTH);

                            return (
                                <div key={item.id} className="rounded-2xl border border-white/10 bg-black/20 p-4">
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0">
                                            <p className="font-mono text-sm font-semibold text-white">{symbol.toLowerCase()}</p>
                                            <p className="mt-1 text-sm text-white/55">{companyName}</p>
                                        </div>
                                        <div className="text-right">
                                            <p className="text-sm font-semibold text-white">
                                                {currentPrice != null ? currentPrice.toFixed(2) : '—'}
                                            </p>
                                            <p className={`mt-1 text-xs font-medium ${textClass}`}>
                                                {currentPrice != null ? (
                                                    <>
                                                        {isPositive ? '+' : ''}{(currentPrice * changePercent / 100).toFixed(2)}
                                                        {' '}
                                                        ({isPositive ? '+' : ''}{changePercent.toFixed(2)}%)
                                                    </>
                                                ) : '—'}
                                            </p>
                                        </div>
                                    </div>

                                    <div className="mt-4 grid grid-cols-2 gap-3">
                                        <div className="rounded-xl border border-white/8 bg-white/[0.03] p-3">
                                            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('watchlist.latestDate')}</p>
                                            <p className="mt-2 text-sm font-medium text-white/80">
                                                {latestQuotes[symbol]?.trading_date || '—'}
                                            </p>
                                        </div>
                                        <div className="rounded-xl border border-white/8 bg-white/[0.03] p-3">
                                            <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{t('watchlist.turnoverRate')}</p>
                                            <p className="mt-2 text-sm font-medium text-white/80">
                                                {latestQuotes[symbol]?.turnover_rate != null
                                                    ? `${(latestQuotes[symbol]!.turnover_rate! * 100).toFixed(2)}%`
                                                    : '—'}
                                            </p>
                                        </div>
                                    </div>

                                    <div className="mt-4 rounded-xl border border-white/10 bg-white/[0.03] px-3 py-3">
                                        <div className="flex items-center justify-between gap-3">
                                            <button
                                                type="button"
                                                onClick={() => standardReport ? void handleOpen(standardReport) : undefined}
                                                disabled={!standardReport}
                                                className="inline-flex h-7 items-center rounded-lg border border-white/10 bg-white/[0.06] px-3 text-xs font-semibold text-white/80 transition-colors hover:bg-white/10 hover:text-white disabled:cursor-default disabled:opacity-35"
                                            >
                                                {copy.open}
                                            </button>
                                            <span className={`inline-flex h-7 items-center rounded-lg border px-2.5 text-xs font-semibold ${
                                                standardReport ? standardReportStatusStyles.ready : standardReportStatusStyles.empty
                                            }`}>
                                                {standardReport ? copy.generated : copy.notGenerated}
                                            </span>
                                        </div>
                                        <div className="mt-2 flex items-center justify-between gap-3 text-xs">
                                            <span className="text-white/45">{copy.generatedAt}</span>
                                            <span className="truncate text-right text-white/65">{standardReport ? formatUpdatedAt(standardReport.updated_at) : '—'}</span>
                                        </div>
                                    </div>

                                    <div className="mt-4">
                                        {actionStatusText ? (
                                            <div className={`flex h-10 w-full items-center justify-center gap-2 rounded-xl border px-3 text-xs font-semibold ${getReportJobStatusClass(rowJob)}`}>
                                                {isActiveReportJob(rowJob) ? (
                                                    <span className="material-symbols-outlined animate-spin text-[18px]">progress_activity</span>
                                                ) : null}
                                                <span>{actionStatusText}</span>
                                            </div>
                                        ) : (
                                            <button
                                                onClick={() => void handleGenerate(item)}
                                                className="flex h-10 w-full items-center justify-center gap-1 rounded-xl border border-primary/20 bg-primary/10 text-xs font-medium text-primary transition-colors hover:bg-primary/15 disabled:opacity-50"
                                                disabled={!serviceReady || !aiModelReady || isGeneratingRow || isLoading}
                                                title={aiModelReady ? copy.generateStandard : copy.configureAIModel}
                                            >
                                                <span className={`material-symbols-outlined text-[18px] ${isGeneratingRow ? 'animate-spin' : ''}`}>
                                                    {isGeneratingRow ? 'progress_activity' : 'auto_stories'}
                                                </span>
                                                <span>{isGeneratingRow ? copy.generating : copy.generateStandard}</span>
                                            </button>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>

                    <div className="hidden overflow-x-auto overscroll-x-contain rounded-xl border border-[#2D2D2D] bg-black/20 touch-pan-x md:block">
                        <table className="min-w-[980px] w-full">
                            <thead className="border-b border-b-[#2D2D2D]">
                                <tr>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">{t('watchlist.ticker')}</th>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">{t('watchlist.latestDate')}</th>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal hidden sm:table-cell">{t('watchlist.lastPrice')}</th>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">{t('watchlist.todayChange')}</th>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">{t('watchlist.turnoverRate')}</th>
                                    <th className="px-4 py-3 text-left text-white/60 text-sm font-medium leading-normal">{copy.report}</th>
                                    <th className="px-4 py-3 text-center text-white/60 text-sm font-medium leading-normal">{t('watchlist.actions')}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {filteredItems.map(item => {
                                    const symbol = item.stock.symbol;
                                    const uziTicker = normalizeUZITicker(symbol);
                                    const companyName = item.stock.company_name;
                                    const changePercent = latestQuotes[symbol]?.change_percent ?? item.current_price?.change_percent ?? 0;
                                    const isPositive = changePercent >= 0;
                                        const { textClass } = getChangeColors(isPositive, language);
                                        const currentPrice = latestQuotes[symbol]?.latest_price ?? item.current_price?.price;
                                        const rowJob = reportJobsByTicker[uziTicker];
                                        const isGeneratingRow = isActiveReportJob(rowJob) || generatingSymbol === uziTicker;
                                        const actionStatusText = getReportJobStatusText(rowJob);
                                        const standardReport = getLatestReportByDepth(reportsByTicker[uziTicker], STANDARD_REPORT_DEPTH);

                                    return (
                                        <tr key={item.id} className="border-t border-t-[#2D2D2D]">
                                            <td className="h-[72px] px-4 py-2 text-white text-sm font-normal leading-normal">
                                                <span className="font-bold">{symbol.toLowerCase()}</span><br />
                                                <span className="text-xs text-white/60">{companyName}</span>
                                            </td>
                                            <td className="h-[72px] px-4 py-2 text-white/60 text-sm">
                                                {latestQuotes[symbol]?.trading_date || '—'}
                                            </td>
                                            <td className="h-[72px] px-4 py-2 text-white/80 text-sm font-normal leading-normal hidden sm:table-cell">
                                                {currentPrice != null ? currentPrice.toFixed(2) : '—'}
                                            </td>
                                            <td className={`h-[72px] px-4 py-2 text-sm font-normal leading-normal ${textClass}`}>
                                                {currentPrice != null ? (
                                                    <>
                                                        {isPositive ? '+' : ''}{(currentPrice * changePercent / 100).toFixed(2)} ({isPositive ? '+' : ''}{changePercent.toFixed(2)}%)
                                                    </>
                                                ) : '—'}
                                            </td>
                                            <td className="h-[72px] px-4 py-2 text-white/60 text-sm">
                                                {latestQuotes[symbol]?.turnover_rate != null ? `${(latestQuotes[symbol]!.turnover_rate! * 100).toFixed(2)}%` : '—'}
                                            </td>
                                            <td className="h-[72px] px-4 py-2 text-sm">
                                                <div className="flex min-w-[220px] flex-col gap-1">
                                                    <div className="flex items-center gap-2">
                                                        <span className={`inline-flex h-7 items-center rounded-lg border px-2.5 text-xs font-semibold ${
                                                            standardReport ? standardReportStatusStyles.ready : standardReportStatusStyles.empty
                                                        }`}>
                                                            {standardReport ? copy.generated : copy.notGenerated}
                                                        </span>
                                                        <button
                                                            type="button"
                                                            onClick={() => standardReport ? void handleOpen(standardReport) : undefined}
                                                            disabled={!standardReport}
                                                            className="inline-flex h-7 items-center rounded-lg border border-white/10 bg-white/[0.06] px-3 text-xs font-semibold text-white/80 transition-colors hover:bg-white/10 hover:text-white disabled:cursor-default disabled:opacity-35"
                                                        >
                                                            {copy.open}
                                                        </button>
                                                    </div>
                                                    <div className="text-xs text-white/55">
                                                        {copy.generatedAt}: {standardReport ? formatUpdatedAt(standardReport.updated_at) : '—'}
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="h-[72px] px-4 py-2 text-center">
                                                <div className="flex items-center justify-center gap-1">
                                                    {actionStatusText ? (
                                                        <span className={`inline-flex h-8 min-w-[72px] items-center justify-center gap-1 rounded-full border px-3 text-xs font-semibold ${getReportJobStatusClass(rowJob)}`}>
                                                            {isActiveReportJob(rowJob) ? (
                                                                <span className="material-symbols-outlined animate-spin text-[16px]">progress_activity</span>
                                                            ) : null}
                                                            {actionStatusText}
                                                        </span>
                                                    ) : (
                                                        <button
                                                            onClick={() => void handleGenerate(item)}
                                                            className="p-2 rounded-full hover:bg-white/10 text-primary transition-colors disabled:opacity-50"
                                                            disabled={!serviceReady || !aiModelReady || isGeneratingRow || isLoading}
                                                            title={aiModelReady ? copy.generateStandard : copy.configureAIModel}
                                                        >
                                                            <span
                                                                className={`material-symbols-outlined ${isGeneratingRow ? 'animate-spin' : ''}`}
                                                                style={{ fontSize: '20px' }}
                                                            >
                                                                {isGeneratingRow ? 'progress_activity' : 'auto_stories'}
                                                            </span>
                                                        </button>
                                                    )}
                                                </div>
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
                            <span className="material-symbols-outlined" style={{ fontSize: '96px' }}>auto_stories</span>
                        </div>
                        <div className="flex max-w-[480px] flex-col items-center gap-2">
                            <p className="text-white text-lg font-bold leading-tight tracking-[-0.015em] text-center">{copy.addWatchFirstTitle}</p>
                            <p className="text-white/60 text-sm font-normal leading-normal text-center">{copy.addWatchFirstDesc}</p>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

export default UZIReports;
