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
    values: [string, string];
}

const Pricing: React.FC = () => {
    const { language } = useLanguage();
    const isZh = language === 'zh';
    const [selectedPlanName, setSelectedPlanName] = useState('VIP');
    const formatPlanPrice = (plan: Plan) => {
        if (plan.price === '0') {
            return isZh ? '免费' : 'Free';
        }
        return `${plan.price}${isZh ? '/月' : '/month'}`;
    };

    const pageCopy = isZh ? {
        planTitle: '会员层级',
        contextTitle: '为什么预测深度很重要',
        compareTitle: '权益对比',
        compareHeaders: ['能力项', '免费等级', 'VIP 会员'],
        plans: [
            {
                name: '免费等级',
                badge: '基础跟踪',
                icon: 'bolt',
                watchlistLimit: '3',
                contextLen: '1024',
                price: '0',
                originalPrice: '',
                summary: '适合轻量跟踪少量核心标的，保留基础预测深度，满足日常观察。',
                highlights: ['最多 3 个关注', '最大上下文 1024', '适合基础跟踪'],
            },
            {
                name: 'VIP',
                badge: '进阶会员',
                icon: 'timeline',
                watchlistLimit: '30',
                contextLen: '2048',
                price: '49',
                originalPrice: '99',
                summary: '适合管理更完整的自选池，并使用更长上下文做多标的比较和持续跟踪。',
                highlights: ['最多 30 个关注', '最大上下文 2048', '适合组合观察与深度研究'],
            },
        ] satisfies Plan[],
        contextCards: [
            {
                title: '1024: 免费最大上下文',
                description: '适合快速判断近期走势变化，覆盖基础研究和轻量跟踪场景。',
            },
            {
                title: '2048: VIP 最大上下文',
                description: '适合观察更长周期的走势延续与阶段变化，帮助做更完整的多标的判断。',
            },
        ],
        comparison: [
            { label: '可关注数量', values: ['3', '30'] },
            { label: '最大上下文', values: ['1024', '2048'] },
            { label: '价格', values: ['免费', '49/月'] },
            { label: '适合的研究方式', values: ['轻量跟踪', '组合观察'] },
            { label: '适合用户', values: ['日常关注用户', '进阶投资者'] },
        ] satisfies ComparisonItem[],
    } : {
        planTitle: 'Membership Layers',
        contextTitle: 'Why Prediction Depth Matters',
        compareTitle: 'Plan Comparison',
        compareHeaders: ['Capability', 'Free', 'VIP'],
        plans: [
            {
                name: 'Free',
                badge: 'Basic Tracking',
                icon: 'bolt',
                watchlistLimit: '3',
                contextLen: '1024',
                price: '0',
                originalPrice: '',
                summary: 'Good for tracking a small core list with enough prediction depth for daily review.',
                highlights: ['Up to 3 watchlist items', 'Max context 1024', 'Best for basic tracking'],
            },
            {
                name: 'VIP',
                badge: 'Advanced Membership',
                icon: 'timeline',
                watchlistLimit: '30',
                contextLen: '2048',
                price: '49',
                originalPrice: '99',
                summary: 'Built for broader watchlists, deeper context, and repeated comparison across multiple ideas.',
                highlights: ['Up to 30 watchlist items', 'Max context 2048', 'Best for portfolio tracking'],
            },
        ] satisfies Plan[],
        contextCards: [
            {
                title: '1024: Free max context',
                description: 'Best when you want a quick read on recent market movement and lightweight tracking.',
            },
            {
                title: '2048: VIP max context',
                description: 'Useful when broader trend continuity and multi-symbol comparison matter more.',
            },
        ],
        comparison: [
            { label: 'Watchlist capacity', values: ['3', '30'] },
            { label: 'Max context', values: ['1024', '2048'] },
            { label: 'Price', values: ['Free', '49/month'] },
            { label: 'Research style', values: ['Light monitoring', 'Portfolio tracking'] },
            { label: 'Best suited for', values: ['Casual investors', 'Advanced users'] },
        ] satisfies ComparisonItem[],
    };
    const selectedPlan = pageCopy.plans.find((plan) => plan.name === selectedPlanName) ?? pageCopy.plans[0];

    return (
        <main className="flex flex-col gap-8 pb-8 sm:gap-14">
            <section>
                <div className="mb-5 sm:mb-8">
                    <h2 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">{pageCopy.planTitle}</h2>
                </div>
                <div className="grid grid-cols-1 gap-5 xl:grid-cols-2">
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
                                        <p className="text-lg font-black text-white/86">{formatPlanPrice(plan)}</p>
                                        {plan.originalPrice && <p className="text-xs text-white/36 line-through">{plan.originalPrice}{isZh ? '/月' : '/month'}</p>}
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

                            <p className={`${mobileDetailClass} mt-4 text-sm leading-6 text-white/68 sm:mt-5 sm:min-h-[56px] sm:leading-7`}>{plan.summary}</p>

                            <ul className={`${mobileDetailClass} mt-3 mb-6 space-y-2.5 sm:mb-7 sm:min-h-[104px] sm:space-y-3`}>
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
                                        {formatPlanPrice(plan)}
                                    </span>
                                    {plan.originalPrice && (
                                        <span className="text-xs leading-none text-white/38 line-through">
                                            {plan.originalPrice}{isZh ? '/月' : '/month'}
                                        </span>
                                    )}
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
                                <p className="text-lg font-black text-amber-100">{formatPlanPrice(selectedPlan)}</p>
                                {selectedPlan.originalPrice && <p className="text-xs text-white/36 line-through">{selectedPlan.originalPrice}{isZh ? '/月' : '/month'}</p>}
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <section className="rounded-[22px] border border-white/10 bg-white/[0.03] p-4 sm:rounded-[26px] sm:p-8">
                <div className="max-w-3xl">
                    <h2 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">{pageCopy.contextTitle}</h2>
                </div>
                <div className="mt-5 grid grid-cols-1 gap-3 sm:mt-8 sm:gap-4 lg:grid-cols-2">
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
                            <div className="mt-3 grid grid-cols-2 gap-2">
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
                    <table className="w-full min-w-[560px]">
                        <thead>
                            <tr className="bg-white/[0.05] text-left">
                                {pageCopy.compareHeaders.map((header, index) => (
                                    <th key={header} className={`px-6 py-4 text-sm font-semibold text-white ${index === 0 ? 'w-[40%]' : 'w-[30%]'}`}>
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
