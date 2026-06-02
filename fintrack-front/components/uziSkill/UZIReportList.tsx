import React from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { UZIReportItem } from '../../services/apiService';
import { formatBytes, formatUpdatedAt, getDepthMeta, getReportDisplayTitle, normalizeReportDepth } from './uziUtils';

interface UZIReportListProps {
    reports: UZIReportItem[];
    isLoading: boolean;
    deletingPath: string | null;
    serviceReady: boolean;
    onOpenReport: (relativePath?: string) => void;
    onDeleteReport: (report: UZIReportItem) => void;
}

const UZIReportList: React.FC<UZIReportListProps> = ({
    reports,
    isLoading,
    deletingPath,
    serviceReady,
    onOpenReport,
    onDeleteReport,
}) => {
    const { t, language } = useLanguage();
    const isZh = language === 'zh';

    if (isLoading) {
        return (
            <div className="flex items-center justify-center rounded-2xl border border-white/10 bg-white/[0.02] px-6 py-16 text-white/50">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-white/20 border-t-primary"></div>
            </div>
        );
    }

    if (reports.length === 0) {
        return (
            <div className="rounded-[26px] border border-dashed border-white/10 bg-white/[0.02] px-6 py-16 text-center text-sm text-white/45">
                {serviceReady
                    ? t('uzi.empty', isZh ? '还没有研报，先输入一只股票试试。' : 'No reports yet. Enter a ticker to generate one.')
                    : (isZh ? '研报服务暂时不可用，恢复后即可继续生成。' : 'The report service is temporarily unavailable. You can continue when it recovers.')}
            </div>
        );
    }

    return (
        <div className="space-y-3">
            {reports.map(report => (
                <div
                    key={report.report_relative_path}
                    className="group flex flex-col gap-4 rounded-[24px] border border-white/10 bg-[linear-gradient(135deg,rgba(255,255,255,0.05),rgba(255,255,255,0.02))] p-4 transition-colors hover:border-white/20 lg:flex-row lg:items-center lg:justify-between"
                >
                    <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                            <span className="rounded-full bg-primary/15 px-2.5 py-1 text-[11px] font-semibold text-primary">
                                {report.ticker}
                            </span>
                            {report.depth ? (
                                <span className="rounded-full bg-white/6 px-2.5 py-1 text-[11px] text-white/60">
                                    {getDepthMeta(normalizeReportDepth(report.depth), language).label}
                                </span>
                            ) : null}
                            <span className="rounded-full bg-white/6 px-2.5 py-1 text-[11px] text-white/55">
                                {report.date_tag || report.directory_name}
                            </span>
                        </div>
                        <p className="mt-3 truncate text-base font-semibold text-white">
                            {getReportDisplayTitle(report.ticker, report.date_tag, language)}
                        </p>
                        <p className="mt-1 text-xs text-white/45">
                            {formatUpdatedAt(report.updated_at)} · {formatBytes(report.size_bytes)}
                        </p>
                    </div>

                    <div className="flex shrink-0 flex-wrap gap-2">
                        <button
                            type="button"
                            onClick={() => onOpenReport(report.report_relative_path)}
                            className="rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-black transition-opacity hover:opacity-90"
                        >
                            {t('uzi.open', isZh ? '阅读研报' : 'Read Report')}
                        </button>
                        <button
                            type="button"
                            onClick={() => onDeleteReport(report)}
                            disabled={deletingPath === report.report_relative_path}
                            className="rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-sm font-medium text-white/65 transition-colors hover:bg-white/10 hover:text-white disabled:opacity-50"
                        >
                            {deletingPath === report.report_relative_path
                                ? t('uzi.deleting', isZh ? '移除中...' : 'Removing...')
                                : t('uzi.delete', isZh ? '移出列表' : 'Remove')}
                        </button>
                    </div>
                </div>
            ))}
        </div>
    );
};

export default UZIReportList;
