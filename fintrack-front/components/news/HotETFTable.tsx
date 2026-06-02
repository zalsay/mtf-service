import React from 'react';
import { FinanceNewsItem } from '../../services/apiService';

interface HotETFTableProps {
  items: FinanceNewsItem[];
  isLoading: boolean;
  emptyText: string;
  loadingText: string;
  openText: string;
  isZh: boolean;
}

const HotETFTable: React.FC<HotETFTableProps> = ({ items, isLoading, emptyText, loadingText, openText, isZh }) => {
  const labels = {
    rank: isZh ? '序号' : 'No.',
    etf: isZh ? 'ETF' : 'ETF',
    rps: 'RPS',
    trend: isZh ? '月/周/日评分' : 'M/W/D',
    stopLoss: isZh ? '防守止损' : 'Stop',
    score: isZh ? '总分' : 'Score',
    status: isZh ? '状态' : 'Status',
    action: isZh ? '操作' : 'Action',
  };

  if (isLoading && items.length === 0) {
    return <div className="rounded-lg border border-white/10 bg-white/[0.03] p-6 text-sm text-white/58">{loadingText}</div>;
  }

  if (items.length === 0) {
    return <div className="rounded-lg border border-white/10 bg-white/[0.03] p-6 text-sm text-white/58">{emptyText}</div>;
  }

  return (
    <section className="overflow-hidden rounded-lg border border-white/[0.08] bg-white/[0.026] text-left">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[940px] table-fixed border-collapse text-left">
          <colgroup>
            <col className="w-14" />
            <col className="w-48" />
            <col className="w-24" />
            <col className="w-36" />
            <col className="w-28" />
            <col className="w-24" />
            <col className="w-24" />
            <col className="w-24" />
          </colgroup>
          <thead className="border-b border-white/[0.08] bg-black/[0.16] text-left text-xs font-bold uppercase tracking-normal text-white/42">
            <tr>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.rank}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.etf}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.rps}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.trend}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.stopLoss}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.score}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.status}</th>
              <th className="px-4 py-3 text-left align-top font-bold">{labels.action}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.07] text-left">
            {items.map((item, index) => (
              <tr key={item.id || item.symbol || item.title} className="text-left transition hover:bg-white/[0.045]">
                <td className="px-4 py-4 text-left align-top">
                  <span className="inline-flex h-7 min-w-7 items-center justify-center rounded-md bg-primary/12 px-2 text-xs font-black text-primary">
                    {index + 1}
                  </span>
                </td>
                <td className="px-4 py-4 text-left align-top">
                  <div className="truncate text-sm font-bold text-white">{item.stock_name || item.title}</div>
                  {item.symbol && <div className="mt-1 font-mono text-xs text-primary/82">{item.symbol}</div>}
                </td>
                <td className="px-4 py-4 text-left align-top font-mono text-sm font-bold text-white">{item.etf_rps || '-'}</td>
                <td className="px-4 py-4 text-left align-top font-mono text-xs leading-6 text-white/68">
                  {item.etf_month || '-'} / {item.etf_week || '-'} / {item.etf_day || '-'}
                </td>
                <td className="px-4 py-4 text-left align-top font-mono text-sm text-white/68">{item.etf_stop_loss || '-'}</td>
                <td className="px-4 py-4 text-left align-top font-mono text-sm font-bold text-primary">{item.etf_score || '-'}</td>
                <td className="px-4 py-4 text-left align-top">
                  <span className="inline-flex h-7 items-center rounded-md border border-primary/20 bg-primary/12 px-2.5 text-xs font-bold text-primary">
                    {item.etf_status || '-'}
                  </span>
                </td>
                <td className="px-4 py-4 text-left align-top">
                  {item.url && (
                    <a
                      href={item.url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex h-8 items-center gap-1 rounded-lg bg-primary/12 px-2.5 text-xs font-bold text-primary transition hover:bg-primary/20"
                    >
                      {openText}
                      <span className="material-symbols-outlined text-sm">open_in_new</span>
                    </a>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
};

export default HotETFTable;
