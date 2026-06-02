import React, { useState, useEffect } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';

interface BindStrategyModalProps {
    isOpen: boolean;
    onClose: () => void;
    symbol: string;
    strategies: any[];
    currentStrategyKey?: string;
    onBind: (symbol: string, strategyKey: string) => void;
}

const BindStrategyModal: React.FC<BindStrategyModalProps> = ({
    isOpen,
    onClose,
    symbol,
    strategies,
    currentStrategyKey,
    onBind
}) => {
    const { t, language } = useLanguage();
    const [selectedStrategy, setSelectedStrategy] = useState<string>(currentStrategyKey || '');
    const [activeTab, setActiveTab] = useState<'official' | 'custom'>('official');
    const [searchTerm, setSearchTerm] = useState('');

    useEffect(() => {
        setSelectedStrategy(currentStrategyKey || '');
    }, [currentStrategyKey, isOpen]);

    if (!isOpen) return null;

    const handleSave = () => {
        if (selectedStrategy) {
            onBind(symbol, selectedStrategy);
            onClose();
        }
    };

    const normalizedSearch = searchTerm.trim().toLowerCase();
    const filteredStrategies = strategies.filter(s => {
        if (activeTab === 'official') return s.is_public === 1;
        return s.is_public !== 1;
    }).filter(s => {
        if (!normalizedSearch) return true;
        const haystack = [
            s.name,
            s.unique_key,
            s.buy_threshold_pct,
            s.sell_threshold_pct,
        ].map(value => String(value ?? '').toLowerCase()).join(' ');
        return haystack.includes(normalizedSearch);
    });

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm">
            <div className="bg-[#1E1E1E] rounded-2xl border border-white/10 w-full max-w-md p-6 shadow-2xl relative overflow-hidden">
                 {/* Background Accents */}
                 <div className="absolute top-0 right-0 w-64 h-64 bg-primary/5 rounded-full blur-3xl -translate-y-1/2 translate-x-1/2 pointer-events-none"></div>

                <div className="relative z-10">
                    <div className="flex justify-between items-start mb-6">
                        <div>
                            <h2 className="text-2xl font-bold text-white mb-1">{language === 'zh' ? '绑定策略' : 'Bind Strategy'}</h2>
                            <p className="text-white/60 text-sm">
                                {language === 'zh' ? `为 ${symbol} 选择应用策略` : `Select strategy for ${symbol}`}
                            </p>
                        </div>
                        <button onClick={onClose} className="text-white/40 hover:text-white transition-colors">
                            <span className="material-symbols-outlined">close</span>
                        </button>
                    </div>

                    <div className="space-y-4">
                        {/* Tabs */}
                        <div className="flex gap-1 p-1 bg-white/5 rounded-lg mb-2">
                            <button
                                onClick={() => setActiveTab('official')}
                                className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-all ${
                                    activeTab === 'official' 
                                        ? 'bg-white/10 text-white shadow-sm' 
                                        : 'text-white/40 hover:text-white/60'
                                }`}
                            >
                                {language === 'zh' ? '官方推荐' : 'Official'}
                            </button>
                            <button
                                onClick={() => setActiveTab('custom')}
                                className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-all ${
                                    activeTab === 'custom' 
                                        ? 'bg-white/10 text-white shadow-sm' 
                                        : 'text-white/40 hover:text-white/60'
                                }`}
                            >
                                {language === 'zh' ? '我的策略' : 'My Strategies'}
                            </button>
                        </div>

                        <label className="flex h-11 items-center gap-2 rounded-lg border border-white/10 bg-white/5 px-3 transition-colors focus-within:border-primary/70 focus-within:bg-white/[0.07]">
                            <span className="material-symbols-outlined text-[18px] text-white/45">search</span>
                            <input
                                value={searchTerm}
                                onChange={(event) => setSearchTerm(event.target.value)}
                                placeholder={language === 'zh' ? '搜索策略名称或参数' : 'Search strategy or parameters'}
                                className="h-full min-w-0 flex-1 border-0 bg-transparent text-sm text-white outline-none placeholder:text-white/35 focus:border-0 focus:outline-none focus:ring-0"
                            />
                            {searchTerm && (
                                <button
                                    type="button"
                                    onClick={() => setSearchTerm('')}
                                    className="flex h-7 w-7 items-center justify-center rounded-md text-white/45 transition-colors hover:bg-white/10 hover:text-white"
                                    aria-label={language === 'zh' ? '清空搜索' : 'Clear search'}
                                >
                                    <span className="material-symbols-outlined text-[16px]">close</span>
                                </button>
                            )}
                        </label>

                        <div className="space-y-2">
                            <div className="max-h-60 overflow-y-auto space-y-2 pr-1 custom-scrollbar">
                                {filteredStrategies.length === 0 ? (
                                    <div className="text-center py-8 text-white/20 italic border border-dashed border-white/10 rounded-lg">
                                        {language === 'zh' ? '暂无可用策略' : 'No strategies available'}
                                    </div>
                                ) : (
                                    filteredStrategies.map((strategy) => (
                                        <div 
                                            key={strategy.unique_key}
                                            onClick={() => setSelectedStrategy(strategy.unique_key)}
                                            className={`
                                                p-3 rounded-lg border cursor-pointer transition-all flex items-center justify-between group
                                                ${selectedStrategy === strategy.unique_key 
                                                    ? 'bg-primary/10 border-primary/50' 
                                                    : 'bg-white/5 border-white/5 hover:bg-white/10 hover:border-white/20'}
                                            `}
                                        >
                                            <div className="flex flex-col">
                                                <span className={`font-medium ${selectedStrategy === strategy.unique_key ? 'text-white' : 'text-white/80'}`}>
                                                    {strategy.name || 'Unnamed'}
                                                </span>
                                                <span className="text-xs text-white/40">
                                                    Buy: {strategy.buy_threshold_pct}% | Sell: {strategy.sell_threshold_pct}%
                                                </span>
                                            </div>
                                            
                                            {selectedStrategy === strategy.unique_key && (
                                                <span className="material-symbols-outlined text-primary">check_circle</span>
                                            )}
                                        </div>
                                    ))
                                )}
                            </div>
                        </div>

                        <div className="flex gap-3 mt-6 pt-4 border-t border-white/10">
                            <button 
                                onClick={onClose}
                                className="flex-1 px-4 py-2.5 rounded-xl bg-white/5 text-white/60 font-medium hover:bg-white/10 hover:text-white transition-colors"
                            >
                                {language === 'zh' ? '取消' : 'Cancel'}
                            </button>
                            <button 
                                onClick={handleSave}
                                disabled={!selectedStrategy}
                                className="flex-1 px-4 py-2.5 rounded-xl bg-primary text-[#0A0A0A] font-bold hover:bg-primary/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                {language === 'zh' ? '确认绑定' : 'Confirm Bind'}
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default BindStrategyModal;
