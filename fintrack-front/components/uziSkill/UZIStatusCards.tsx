import React from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { UZIAnalyzeResponse, UZIHealthResponse } from '../../services/apiService';
import { getAnalyzeSummary } from './uziUtils';

interface UZIStatusCardsProps {
    health: UZIHealthResponse | null;
    healthError: string | null;
    reportCount: number;
    lastAnalyze: UZIAnalyzeResponse | null;
    onOpenLatest?: () => void;
}

const cardClassName = 'rounded-2xl border border-white/10 bg-white/[0.035] p-4 backdrop-blur-sm';

const badgeClassName = (isReady: boolean) => (
    isReady
        ? 'bg-primary/15 text-primary'
        : 'bg-amber-500/15 text-amber-200'
);

const UZIStatusCards: React.FC<UZIStatusCardsProps> = ({
    health,
    healthError,
    reportCount,
    lastAnalyze,
    onOpenLatest,
}) => {
    const { language } = useLanguage();
    const isZh = language === 'zh';
    const isReady = health?.status === 'ok' && !healthError;
    const latestReportPath = lastAnalyze?.report?.report_relative_path || lastAnalyze?.report_relative_path;
    const items = [
        {
            id: 'status',
            label: isZh ? '状态' : 'Status',
            value: isReady ? (isZh ? '可用' : 'Available') : (isZh ? '稍后再试' : 'Try Later'),
            tone: isReady ? 'text-primary' : 'text-amber-200',
            hint: isReady ? (isZh ? '可直接生成' : 'Ready to generate') : (isZh ? '仍可查看历史' : 'History remains available'),
        },
        {
            id: 'generated',
            label: isZh ? '已生成' : 'Generated',
            value: String(reportCount),
            tone: 'text-white',
            hint: isZh ? '我的研报' : 'My reports',
        },
        {
            id: 'latest',
            label: isZh ? '最近' : 'Latest',
            value: getAnalyzeSummary(lastAnalyze, language),
            tone: 'text-white',
            hint: lastAnalyze ? (isZh ? '已更新' : 'Updated') : (isZh ? '暂无记录' : 'No records'),
        },
    ];

    return (
        <div className="grid gap-3 lg:grid-cols-3">
            {items.map(item => (
                <div key={item.label} className={cardClassName}>
                    <div className="flex items-start justify-between gap-3">
                        <div>
                            <p className="text-xs uppercase tracking-[0.18em] text-white/35">{item.label}</p>
                            <p className={`mt-3 text-2xl font-black tracking-[-0.03em] ${item.tone}`}>{item.value}</p>
                            <p className="mt-1 text-xs text-white/45">{item.hint}</p>
                        </div>
                        {item.id === 'status' ? (
                            <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${badgeClassName(isReady)}`}>
                                {isReady ? 'ON' : 'OFF'}
                            </span>
                        ) : null}
                        {item.id === 'latest' && latestReportPath && onOpenLatest ? (
                            <button
                                type="button"
                                onClick={onOpenLatest}
                                className="rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-xs font-medium text-white/80 transition-colors hover:bg-white/10 hover:text-white"
                            >
                                {isZh ? '打开' : 'Open'}
                            </button>
                        ) : null}
                    </div>
                </div>
            ))}
        </div>
    );
};

export default UZIStatusCards;
