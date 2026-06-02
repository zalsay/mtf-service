import React, { useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';

interface Plan {
    name: string;
    badge: string;
    icon: string;
    watchlistLimit: string;
    contextLen: string;
    price: string;
    originalPrice: string;
    summary: string;
    highlights: string[];
}

interface ComparisonItem {
    label: string;
    values: [string, string, string];
}

const Pricing: React.FC = () => {
    const { language } = useLanguage();
    const isZh = language === 'zh';
    const [selectedPlanName, setSelectedPlanName] = useState('SVIP');

    const pageCopy = isZh ? {
        planTitle: '会员层级',
        contextTitle: '为什么预测深度很重要',
        compareTitle: '权益对比',
        compareHeaders: ['能力项', 'VIP', 'SVIP', 'UVIP'],
        plans: [
            {
                name: 'VIP',
                badge: '入门研究',
                icon: 'bolt',
                watchlistLimit: '3',
                contextLen: '512',
                price: '99',
                originalPrice: '199',
                summary: '适合用较少标的做日常跟踪，快速筛选近期更值得看的机会。',
                highlights: ['覆盖核心自选池', '预测深度偏轻，响应更直接', '适合短线或轻量观察'],
            },
            {
                name: 'SVIP',
                badge: '进阶策略',
                icon: 'timeline',
                watchlistLimit: '10',
                contextLen: '1024',
                price: '199',
                originalPrice: '399',
                summary: '在覆盖更多标的的同时，也能看到更均衡的走势信息，适合波段与多标的比较。',
                highlights: ['可维护中等规模股票池', '预测深度更均衡', '适合主力自选组合管理'],
            },
            {
                name: 'UVIP',
                badge: '专业深研',
                icon: 'neurology',
                watchlistLimit: '50',
                contextLen: '2048',
                price: '299',
                originalPrice: '599',
                summary: '面向高频研究和多策略用户，提供最大关注容量与最完整的预测信息参考。',
                highlights: ['覆盖更完整市场观察池', '预测深度最高', '适合专业用户持续研究'],
            },
        ] satisfies Plan[],
        contextCards: [
            {
                title: '512: 轻量预测深度',
                description: '适合快速判断近期走势变化，重点看最近阶段的市场表现。',
            },
            {
                title: '1024: 均衡预测深度',
                description: '在近期变化和更长走势之间取得平衡，更适合做多标的比较。',
            },
            {
                title: '2048: 深度预测',
                description: '适合观察更长周期的走势延续与阶段变化，帮助做更完整的判断。',
            },
        ],
        comparison: [
            { label: '可关注数量', values: ['3', '10', '50'] },
            { label: '预测深度', values: ['512', '1024', '2048'] },
            { label: '适合的研究方式', values: ['轻量跟踪', '组合观察', '深度研究'] },
            { label: '看盘范围', values: ['近期为主', '近期 + 中期', '更长走势'] },
            { label: '适合用户', values: ['日常关注用户', '进阶投资者', '专业研究者'] },
        ] satisfies ComparisonItem[],
    } : {
        planTitle: 'Membership Layers',
        contextTitle: 'Why Prediction Depth Matters',
        compareTitle: 'Plan Comparison',
        compareHeaders: ['Capability', 'VIP', 'SVIP', 'UVIP'],
        plans: [
            {
                name: 'VIP',
                badge: 'Starter Research',
                icon: 'bolt',
                watchlistLimit: '3',
                contextLen: '512',
                price: '99',
                originalPrice: '199',
                summary: 'Good for a compact watchlist and fast review of near-term opportunities.',
                highlights: ['Covers a focused core list', 'Lighter prediction depth for quick decisions', 'Best for lightweight monitoring'],
            },
            {
                name: 'SVIP',
                badge: 'Advanced Strategy',
                icon: 'timeline',
                watchlistLimit: '10',
                contextLen: '1024',
                price: '199',
                originalPrice: '399',
                summary: 'A balanced tier for broader coverage and clearer medium-range reading across multiple ideas.',
                highlights: ['Supports a wider working universe', 'Balanced prediction depth', 'Fits serious portfolio tracking'],
            },
            {
                name: 'UVIP',
                badge: 'Professional Research',
                icon: 'neurology',
                watchlistLimit: '50',
                contextLen: '2048',
                price: '299',
                originalPrice: '599',
                summary: 'Built for power users who want the strongest forecast depth and the broadest market coverage.',
                highlights: ['Covers a much larger research set', 'Deepest prediction depth', 'Best for continuous professional analysis'],
            },
        ] satisfies Plan[],
        contextCards: [
            {
                title: '512: Light prediction depth',
                description: 'Best when you want a quick read on recent market movement and near-term opportunities.',
            },
            {
                title: '1024: Balanced prediction depth',
                description: 'A middle ground that sees both recent moves and a wider trend backdrop.',
            },
            {
                title: '2048: Deep prediction depth',
                description: 'Useful when broader trend continuity and longer-cycle movement matter more.',
            },
        ],
        comparison: [
            { label: 'Watchlist capacity', values: ['3', '10', '50'] },
            { label: 'Prediction depth', values: ['512', '1024', '2048'] },
            { label: 'Research style', values: ['Light monitoring', 'Portfolio tracking', 'Deep research'] },
            { label: 'Market reading range', values: ['Recent only', 'Recent + mid-term', 'Longer trend view'] },
            { label: 'Best suited for', values: ['Casual investors', 'Advanced users', 'Professional researchers'] },
        ] satisfies ComparisonItem[],
    };
    const selectedPlan = pageCopy.plans.find((plan) => plan.name === selectedPlanName) ?? pageCopy.plans[0];

    return (
        <main className="flex flex-col gap-8 pb-8 sm:gap-14">
            <section>
                <div className="mb-5 sm:mb-8">
                    <h2 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">{pageCopy.planTitle}</h2>
                </div>
                <div className="grid grid-cols-1 gap-5 xl:grid-cols-3">
                    {pageCopy.plans.map((plan) => {
                        const isSelected = plan.name === selectedPlanName;
                        const mobileDetailClass = isSelected ? 'block' : 'hidden sm:block';
                        const mobilePriceClass = isSelected ? 'flex' : 'hidden sm:flex';

                        return (
                        <button
                            type="button"
                            key={plan.name}
                            onClick={() => setSelectedPlanName(plan.name)}
                            aria-pressed={isSelected}
                            className={`flex h-full cursor-pointer flex-col rounded-[22px] border p-4 text-left transition-colors sm:rounded-[26px] sm:p-6 ${isSelected
                                ? 'premium-gold-card'
                                : 'border-white/10 bg-white/[0.04] hover:border-white/18 hover:bg-white/[0.06]'
                            }`}
                        >
                            <div className="flex items-start justify-between gap-4">
                                <div>
                                    <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-white/42">{plan.badge}</p>
                                    <h3 className="mt-2 text-xl font-bold text-white sm:text-2xl">{plan.name}</h3>
                                    <div className="mt-2 flex items-baseline gap-2 sm:hidden">
                                        <p className="text-lg font-black text-white/86">{plan.price}{isZh ? '/月' : '/month'}</p>
                                        <p className="text-xs text-white/36 line-through">{plan.originalPrice}{isZh ? '/月' : '/month'}</p>
                                    </div>
                                </div>
                                <div className={`flex h-10 w-10 items-center justify-center rounded-2xl sm:h-11 sm:w-11 ${isSelected ? 'bg-amber-300 text-black' : 'bg-white/8 text-primary'}`}>
                                    <span className="material-symbols-outlined text-[20px] sm:text-[22px]">{plan.icon}</span>
                                </div>
                            </div>

                            <div className="mt-4 grid grid-cols-2 gap-2.5 sm:mt-6 sm:gap-3">
                                <div className="rounded-2xl border border-white/8 bg-black/10 p-3 sm:p-4">
                                    <p className="text-[11px] uppercase tracking-[0.16em] text-white/42">{isZh ? '可关注数' : 'Watchlist Limit'}</p>
                                    <p className="mt-2 text-lg font-bold text-white sm:text-xl">{plan.watchlistLimit}</p>
                                </div>
                                <div className="rounded-2xl border border-white/8 bg-black/10 p-3 sm:p-4">
                                    <p className="text-[11px] uppercase tracking-[0.16em] text-white/42">{isZh ? '预测深度' : 'Prediction Depth'}</p>
                                    <p className="mt-2 text-lg font-bold text-white sm:text-xl">{plan.contextLen}</p>
                                </div>
                            </div>

                            <p className={`${mobileDetailClass} mt-4 text-sm leading-6 text-white/68 sm:mt-5 sm:min-h-[56px] sm:leading-7 xl:min-h-[84px] 2xl:min-h-[56px]`}>{plan.summary}</p>

                            <ul className={`${mobileDetailClass} mt-3 mb-6 space-y-2.5 sm:mb-7 sm:min-h-[104px] sm:space-y-3 xl:min-h-[116px] 2xl:min-h-[104px]`}>
                                {plan.highlights.map((item) => (
                                    <li key={item} className="flex items-center gap-3 text-sm text-white/78">
                                        <span className={`material-symbols-outlined inline-flex h-5 w-5 shrink-0 items-center justify-center text-base leading-none ${isSelected ? 'text-amber-200' : 'text-primary'}`}>check_circle</span>
                                        <span className="leading-5">{item}</span>
                                    </li>
                                ))}
                            </ul>

                            <div className={`${mobilePriceClass} mt-5 h-16 items-center justify-between rounded-xl border px-4 text-sm sm:mt-auto ${isSelected ? 'border-amber-200/25 bg-amber-300/10 text-white' : 'border-white/8 bg-black/10 text-white/52'}`}>
                                <span className="flex h-full items-center font-semibold">{plan.name}</span>
                                <span className="ml-3 flex h-full shrink-0 items-center justify-end gap-2 whitespace-nowrap text-right">
                                    <span className={`text-lg font-black leading-none ${isSelected ? 'text-amber-100' : 'text-white/82'}`}>
                                        {plan.price}{isZh ? '/月' : '/month'}
                                    </span>
                                    <span className="text-xs leading-none text-white/38 line-through">
                                        {plan.originalPrice}{isZh ? '/月' : '/month'}
                                    </span>
                                </span>
                            </div>
                        </button>
                    )})}
                </div>
                <div className="mt-3 rounded-[20px] border border-white/10 bg-white/[0.03] p-4 sm:hidden">
                    <div className="flex items-center justify-between gap-4">
                        <div>
                            <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-white/42">{selectedPlan.badge}</p>
                            <p className="mt-2 text-lg font-bold text-white">{selectedPlan.name}</p>
                        </div>
                        <div className="rounded-xl bg-black/15 px-3 py-2 text-right">
                            <p className="text-[11px] uppercase tracking-[0.16em] text-white/42">{isZh ? '当前方案' : 'Current Plan'}</p>
                            <div className="mt-1 flex items-baseline justify-end gap-2">
                                <p className="text-lg font-black text-amber-100">{selectedPlan.price}{isZh ? '/月' : '/month'}</p>
                                <p className="text-xs text-white/36 line-through">{selectedPlan.originalPrice}{isZh ? '/月' : '/month'}</p>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <section className="rounded-[22px] border border-white/10 bg-white/[0.03] p-4 sm:rounded-[26px] sm:p-8">
                <div className="max-w-3xl">
                    <h2 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">{pageCopy.contextTitle}</h2>
                </div>
                <div className="mt-5 grid grid-cols-1 gap-3 sm:mt-8 sm:gap-4 lg:grid-cols-3">
                    {pageCopy.contextCards.map((item) => (
                        <div key={item.title} className="rounded-2xl border border-white/8 bg-black/10 p-4 sm:p-5">
                            <p className="text-sm font-semibold text-white sm:text-base">{item.title}</p>
                            <p className="mt-2 text-sm leading-6 text-white/62 sm:leading-7">{item.description}</p>
                        </div>
                    ))}
                </div>
            </section>

            <section>
                <div className="mb-5 sm:mb-6">
                    <h2 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">{pageCopy.compareTitle}</h2>
                </div>
                <div className="grid grid-cols-1 gap-2.5 md:hidden">
                    {pageCopy.comparison.map((item) => (
                        <div key={item.label} className="rounded-[22px] border border-white/10 bg-white/[0.03] p-3.5">
                            <p className="text-sm font-semibold text-white">{item.label}</p>
                            <div className="mt-3 grid grid-cols-3 gap-2">
                                {pageCopy.plans.map((plan, index) => (
                                    <div key={`${item.label}-${plan.name}`} className="rounded-2xl border border-white/8 bg-black/10 p-2.5 text-center">
                                        <p className="text-[11px] uppercase tracking-[0.16em] text-white/35">{plan.name}</p>
                                        <p className="mt-2 text-sm font-medium text-white/78">{item.values[index]}</p>
                                    </div>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
                <div className="hidden overflow-x-auto overscroll-x-contain rounded-[24px] border border-white/10 bg-white/[0.03] touch-pan-x md:block">
                    <table className="w-full min-w-[720px]">
                        <thead>
                            <tr className="bg-white/[0.05] text-left">
                                {pageCopy.compareHeaders.map((header, index) => (
                                    <th key={header} className={`px-6 py-4 text-sm font-semibold text-white ${index === 0 ? 'w-[34%]' : 'w-[22%]'}`}>
                                        {header}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {pageCopy.comparison.map((item) => (
                                <tr key={item.label} className="border-t border-white/8">
                                    <td className="px-6 py-4 text-sm font-medium text-white/84">{item.label}</td>
                                    {item.values.map((value) => (
                                        <td key={`${item.label}-${value}`} className="px-6 py-4 text-sm text-white/66">
                                            {value}
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </section>

        </main>
    );
};

export default Pricing;
