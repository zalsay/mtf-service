import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { financeNewsAPI, FinanceNewsCategory, FinanceNewsItem } from '../../services/apiService';
import { useLanguage } from '../../contexts/LanguageContext';
import HotETFTable from './HotETFTable';

interface FinanceNewsProps {
  onAuthError?: () => void;
}

const categories: { id: FinanceNewsCategory; icon: string; zh: string; en: string }[] = [
  { id: 'market', icon: 'newspaper', zh: '市场资讯', en: 'Market' },
  { id: 'global', icon: 'public', zh: '全球财经', en: 'Global' },
  { id: 'stock', icon: 'monitoring', zh: '个股新闻', en: 'Stock' },
  { id: 'announcements', icon: 'campaign', zh: '公司公告', en: 'Announcements' },
  { id: 'lhb', icon: 'leaderboard', zh: '东方财富龙虎榜', en: 'Dragon Tiger' },
];

const inputShellClassName = 'flex h-11 items-center gap-2 rounded-lg border-0 bg-black/[0.16] px-3 shadow-none outline-none ring-0 [box-shadow:none] [outline:none] focus-within:border-0 focus-within:outline-none focus-within:ring-0 focus-within:[box-shadow:none] focus-within:[outline:none]';
const inputClassName = 'appearance-none border-0 bg-transparent text-sm text-white outline-none ring-0 [box-shadow:none] [outline:none] placeholder:text-white/35 focus:border-0 focus:outline-none focus:ring-0 focus:[box-shadow:none] focus:[outline:none] focus-visible:outline-none focus-visible:ring-0 focus-visible:[box-shadow:none] focus-visible:[outline:none]';
const newsPageSize = 20;

const mergeNewsItems = (current: FinanceNewsItem[], next: FinanceNewsItem[]) => {
  const seen = new Set(current.map((item) => item.id || item.url || item.title));
  const uniqueNext = next.filter((item) => {
    const key = item.id || item.url || item.title;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
  return [...current, ...uniqueNext];
};

const newsItemKey = (item: FinanceNewsItem) => item.id || item.url || item.title;

const FinanceNews: React.FC<FinanceNewsProps> = ({ onAuthError }) => {
  const { language } = useLanguage();
  const isZh = language === 'zh';
  const [category, setCategory] = useState<FinanceNewsCategory>('market');
  const [symbol, setSymbol] = useState('688017');
  const [keyword, setKeyword] = useState('');
  const [items, setItems] = useState<FinanceNewsItem[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(true);
  const [error, setError] = useState('');
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const requestSeqRef = useRef(0);
  const isRequestingRef = useRef(false);

  const copy = useMemo(() => ({
    title: isZh ? '财经资讯' : 'Finance News',
    subtitle: isZh ? '实时财经新闻、个股资讯和公司公告' : 'Market headlines, stock news and company announcements',
    search: isZh ? '关键词' : 'Keyword',
    symbol: isZh ? '股票代码' : 'Ticker',
    refresh: isZh ? '刷新' : 'Refresh',
    empty: isZh ? '暂无资讯' : 'No news',
    open: isZh ? '打开原文' : 'Open',
    loading: isZh ? '正在加载资讯...' : 'Loading news...',
    loadingMore: isZh ? '正在加载更多...' : 'Loading more...',
    loadMore: isZh ? '加载更多' : 'Load more',
    loadedCount: isZh ? `已加载 ${items.length} 条` : `${items.length} loaded`,
    noMore: isZh ? '没有更多资讯了' : 'No more news',
    rank: isZh ? '序号' : 'No.',
    date: isZh ? '日期' : 'Date',
    stock: isZh ? '股票' : 'Stock',
    detail: isZh ? '上榜原因与资金' : 'Reason & Flow',
    action: isZh ? '操作' : 'Action',
  }), [isZh, items.length]);

  const loadNews = useCallback(async (targetPage = 1, append = false) => {
    if (append && isRequestingRef.current) {
      return;
    }
    isRequestingRef.current = true;
    const requestSeq = requestSeqRef.current + 1;
    requestSeqRef.current = requestSeq;
    if (append) {
      setIsLoadingMore(true);
    } else {
      setIsLoading(true);
      setHasMore(true);
    }
    setError('');
    try {
      const response = await financeNewsAPI.list({
        category,
        symbol: category === 'stock' || category === 'announcements' ? symbol : undefined,
        keyword: keyword || undefined,
        limit: newsPageSize,
        page: targetPage,
      });
      if (requestSeq !== requestSeqRef.current) {
        return;
      }
      const nextItems = response.items || [];
      setItems((current) => append ? mergeNewsItems(current, nextItems) : nextItems);
      setPage(response.query?.page || targetPage);
      setHasMore(nextItems.length >= newsPageSize);
    } catch (err: any) {
      const message = err?.message || (isZh ? '资讯加载失败' : 'Failed to load news');
      setError(message);
      if (message.includes('401') || message.includes('Unauthorized')) {
        onAuthError?.();
      }
    } finally {
      if (requestSeq === requestSeqRef.current) {
        setIsLoading(false);
        setIsLoadingMore(false);
        isRequestingRef.current = false;
      }
    }
  }, [category, symbol, keyword, isZh, onAuthError]);

  useEffect(() => {
    loadNews(1, false);
  }, [loadNews]);

  const loadNextPage = useCallback(() => {
    if (isLoading || isLoadingMore || isRequestingRef.current || !hasMore) {
      return;
    }
    loadNews(page + 1, true);
  }, [hasMore, isLoading, isLoadingMore, loadNews, page]);

  useEffect(() => {
    const target = loadMoreRef.current;
    if (!target || typeof IntersectionObserver === 'undefined') {
      return undefined;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        loadNextPage();
      }
    }, { rootMargin: '240px 0px 240px 0px' });
    observer.observe(target);
    return () => observer.disconnect();
  }, [loadNextPage]);

  return (
    <div className="flex w-full flex-col gap-5">
      <header className="flex flex-col gap-4 border-b border-white/10 pb-5 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="text-3xl font-black leading-tight text-white sm:text-4xl">{copy.title}</h1>
          <p className="mt-2 text-sm leading-6 text-white/58">{copy.subtitle}</p>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row">
          <div className={inputShellClassName}>
            <span className="material-symbols-outlined text-lg text-white/45">search</span>
            <input
              type="search"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              placeholder={copy.search}
              aria-label={copy.search}
              className={`w-44 ${inputClassName}`}
            />
          </div>
          {(category === 'stock' || category === 'announcements') && (
            <div className={inputShellClassName}>
              <span className="material-symbols-outlined text-lg text-white/45">tag</span>
              <input
                value={symbol}
                onChange={(event) => setSymbol(event.target.value)}
                placeholder={copy.symbol}
                aria-label={copy.symbol}
                className={`w-28 ${inputClassName}`}
              />
            </div>
          )}
          <button
            type="button"
            onClick={() => loadNews(1, false)}
            disabled={isLoading}
            className="inline-flex h-11 items-center justify-center gap-2 rounded-lg bg-primary px-4 text-sm font-bold text-black transition hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <span className={`material-symbols-outlined text-lg ${isLoading ? 'animate-spin' : ''}`}>refresh</span>
            {copy.refresh}
          </button>
        </div>
      </header>

      <div className="flex gap-2 overflow-x-auto pb-1">
        {categories.map((item) => {
          const active = category === item.id;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => setCategory(item.id)}
              className={`flex h-10 shrink-0 items-center gap-2 rounded-lg border px-3 text-sm font-semibold transition ${
                active
                  ? 'border-primary/50 bg-primary/18 text-primary'
                  : 'border-white/10 bg-white/[0.03] text-white/68 hover:bg-white/[0.07] hover:text-white'
              }`}
            >
              <span className="material-symbols-outlined text-lg" style={{ fontVariationSettings: active ? "'FILL' 1" : "" }}>
                {item.icon}
              </span>
              {isZh ? item.zh : item.en}
            </button>
          );
        })}
      </div>

      {error && (
        <div className="rounded-lg border border-red-400/25 bg-red-500/10 px-4 py-3 text-sm text-red-100">
          {error}
        </div>
      )}

      {category === 'hot_etf' ? (
        <HotETFTable
          items={items}
          isLoading={isLoading}
          emptyText={copy.empty}
          loadingText={copy.loading}
          openText={copy.open}
          isZh={isZh}
        />
      ) : category === 'lhb' ? (
        <section className="overflow-hidden rounded-lg border border-white/[0.08] bg-white/[0.026] text-left">
          {isLoading && items.length === 0 ? (
            <div className="p-6 text-sm text-white/58">{copy.loading}</div>
          ) : items.length === 0 ? (
            <div className="p-6 text-sm text-white/58">{copy.empty}</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[920px] table-fixed border-collapse text-left">
                <colgroup>
                  <col className="w-14" />
                  <col className="w-24" />
                  <col className="w-28" />
                  <col />
                  <col className="w-24" />
                </colgroup>
                <thead className="border-b border-white/[0.08] bg-black/[0.16] text-left text-xs font-bold uppercase tracking-normal text-white/42">
                  <tr>
                    <th className="px-4 py-3 text-left align-top font-bold">{copy.rank}</th>
                    <th className="px-4 py-3 text-left align-top font-bold">{copy.date}</th>
                    <th className="px-4 py-3 text-left align-top font-bold">{copy.stock}</th>
                    <th className="px-4 py-3 text-left align-top font-bold">{copy.detail}</th>
                    <th className="px-4 py-3 text-left align-top font-bold">{copy.action}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/[0.07] text-left">
                  {items.map((item, index) => (
                    <tr key={newsItemKey(item)} className="text-left transition hover:bg-white/[0.045]">
                      <td className="px-4 py-4 text-left align-top">
                        <span className="inline-flex h-7 min-w-7 items-center justify-center rounded-md bg-primary/12 px-2 text-xs font-black text-primary">
                          {index + 1}
                        </span>
                      </td>
                      <td className="px-4 py-4 text-left align-top text-sm font-medium text-white/58">
                        {item.published_at ? formatNewsDate(item.published_at) : '-'}
                      </td>
                      <td className="px-4 py-4 text-left align-top">
                        <div className="truncate text-left text-sm font-bold text-white">{item.stock_name || item.symbol || '-'}</div>
                        {item.symbol && <div className="mt-1 truncate text-left font-mono text-xs text-primary/82">{item.symbol}</div>}
                      </td>
                      <td className="px-4 py-4 text-left align-top">
                        {item.summary ? (
                          <p className="line-clamp-2 text-left text-sm leading-6 text-white/[0.62]">{item.summary}</p>
                        ) : (
                          <p className="text-left text-sm text-white/42">-</p>
                        )}
                      </td>
                      <td className="px-4 py-4 text-left align-top">
                        {item.url && (
                          <a
                            href={item.url}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex h-8 items-center gap-1 rounded-lg bg-primary/12 px-2.5 text-xs font-bold text-primary transition hover:bg-primary/20"
                          >
                            {copy.open}
                            <span className="material-symbols-outlined text-sm">open_in_new</span>
                          </a>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
          )}
        </section>
      ) : (
        <section className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
          {isLoading && items.length === 0 ? (
            <div className="col-span-full rounded-lg border border-white/10 bg-white/[0.03] p-6 text-sm text-white/58">{copy.loading}</div>
          ) : items.length === 0 ? (
            <div className="col-span-full rounded-lg border border-white/10 bg-white/[0.03] p-6 text-sm text-white/58">{copy.empty}</div>
          ) : items.map((item) => (
            <article
              key={newsItemKey(item)}
              className="group relative min-h-[214px] overflow-hidden rounded-lg border border-white/[0.08] bg-[linear-gradient(145deg,rgba(255,255,255,0.07),rgba(255,255,255,0.026)_58%,rgba(0,0,0,0.10))] p-5 shadow-[0_18px_42px_rgba(0,0,0,0.20)] transition duration-200 hover:border-primary/[0.28] hover:bg-white/[0.06] hover:shadow-[0_24px_62px_rgba(0,0,0,0.27)]"
            >
              <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/40 to-transparent" />
              <div className="pointer-events-none absolute bottom-0 left-0 right-0 h-px bg-white/[0.06]" />
              {item.url && (
                <a
                  href={item.url}
                  target="_blank"
                  rel="noreferrer"
                  className="absolute right-4 top-4 inline-flex h-8 items-center gap-1 rounded-lg bg-primary/12 px-2.5 text-xs font-bold text-primary transition hover:bg-primary/20 hover:text-primary"
                >
                  {copy.open}
                  <span className="material-symbols-outlined text-sm">open_in_new</span>
                </a>
              )}
              <div className="mb-4 flex min-h-8 flex-wrap items-center gap-2 pr-24 text-xs text-white/45">
                {item.source && (
                  <span className="inline-flex h-7 items-center rounded-md bg-white/[0.06] px-2.5 font-semibold text-white/70">
                    {item.source}
                  </span>
                )}
                {item.published_at && (
                  <span className="inline-flex h-7 items-center gap-1 text-white/45">
                    <span className="material-symbols-outlined text-[15px]">schedule</span>
                    {formatNewsTime(item.published_at)}
                  </span>
                )}
                {item.category && item.category !== 'market' && <span className="rounded bg-white/8 px-2 py-0.5 text-white/55">{item.category}</span>}
                {item.symbol && <span className="rounded bg-primary/14 px-2 py-0.5 text-primary">{item.stock_name || item.symbol}</span>}
              </div>
              <h2 className="pr-14 text-base font-bold leading-6 text-white transition group-hover:text-primary sm:text-[17px]">{item.title}</h2>
              {item.summary && <p className="mt-3 line-clamp-3 text-sm leading-6 text-white/[0.62]">{item.summary}</p>}
            </article>
          ))}
        </section>
      )}

      {items.length > 0 && (
        <div ref={loadMoreRef} className="flex flex-col items-center gap-3 py-2">
          <div className="text-xs font-medium text-white/42">
            {isLoadingMore ? copy.loadingMore : hasMore ? copy.loadedCount : copy.noMore}
          </div>
          {hasMore && (
            <button
              type="button"
              onClick={loadNextPage}
              disabled={isLoadingMore || isLoading}
              className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-white/10 bg-white/[0.045] px-4 text-sm font-semibold text-white/78 transition hover:border-primary/25 hover:bg-white/[0.08] hover:text-white disabled:cursor-not-allowed disabled:opacity-55"
            >
              <span className={`material-symbols-outlined text-[18px] ${isLoadingMore ? 'animate-spin' : ''}`}>
                {isLoadingMore ? 'progress_activity' : 'expand_more'}
              </span>
              {isLoadingMore ? copy.loadingMore : copy.loadMore}
            </button>
          )}
        </div>
      )}
    </div>
  );
};

const formatNewsTime = (value: string) => {
  const normalized = value.replace(/:(\d{3})$/, '');
  const date = new Date(normalized.replace(' ', 'T'));
  if (Number.isNaN(date.getTime())) {
    return value.slice(0, 19);
  }
  return date.toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
};

const formatNewsDate = (value: string) => {
  const normalized = value.replace(/:(\d{3})$/, '');
  const date = new Date(normalized.replace(' ', 'T'));
  if (Number.isNaN(date.getTime())) {
    return value.slice(0, 10);
  }
  return date.toLocaleDateString(undefined, {
    month: '2-digit',
    day: '2-digit',
  });
};

export default FinanceNews;
