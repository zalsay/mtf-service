import React, { useState, useEffect } from 'react';
import { StrategyParams } from '../../types';
import { useLanguage } from '../../contexts/LanguageContext';
import { strategyAPI } from '../../services/apiService';
import AddStrategyModal from './AddStrategyModal';

interface StrategyCardProps {
    uniqueKey: string;
    symbol: string;
    onUpdate?: () => void;
}

const StrategyCard: React.FC<StrategyCardProps> = ({ uniqueKey, symbol, onUpdate }) => {
    const { t, language } = useLanguage();
    const [strategy, setStrategy] = useState<StrategyParams | null>(null);
    const [loading, setLoading] = useState(true);
    const [isModalOpen, setIsModalOpen] = useState(false);

    const fetchStrategy = async () => {
        setLoading(true);
        try {
            const data = await strategyAPI.getParams(uniqueKey);
            if (data && data.unique_key) {
                setStrategy(data);
            } else {
                setStrategy(null);
            }
        } catch (err) {
            // Ignore 404
            setStrategy(null);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        if (uniqueKey) {
            fetchStrategy();
        }
    }, [uniqueKey]);

    const handleSuccess = (params: StrategyParams) => {
        setStrategy(params);
        if (onUpdate) onUpdate();
    };

    if (loading) {
        return (
            <div className="rounded-xl bg-white/5 p-4 animate-pulse h-32 flex items-center justify-center">
                <div className="h-4 w-24 bg-white/10 rounded"></div>
            </div>
        );
    }

    return (
        <>
            <div className="rounded-xl bg-white/5 border border-white/10 overflow-hidden hover:border-white/20 transition-colors group relative">
                <div className="p-5 flex flex-col h-full">
                    <div className="flex justify-between items-start mb-4">
                        <div>
                            <h3 className="text-lg font-bold text-white flex items-center gap-2">
                                {strategy?.name || symbol} 
                                {strategy?.is_public === 1 ? (
                                    <span className="text-xs px-2 py-0.5 rounded-full bg-blue-500/20 text-blue-400 font-medium">
                                        {language === 'zh' ? '官方推荐' : 'Official'}
                                    </span>
                                ) : (
                                    <span className="text-xs px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-400 font-medium">
                                        {language === 'zh' ? '个人策略' : 'Personal'}
                                    </span>
                                )}
                            </h3>
                        </div>
                        {strategy?.is_public !== 1 && (
                            <button 
                                onClick={() => setIsModalOpen(true)}
                                className="w-9 h-9 flex items-center justify-center rounded-lg bg-white/5 text-white/60 hover:bg-white/10 hover:text-white transition-colors"
                            >
                                <span className="material-symbols-outlined text-lg">settings</span>
                            </button>
                        )}
                    </div>

                    {strategy ? (
                        <div className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
                            <div className="flex flex-col">
                                <span className="text-white/40 text-xs">{language === 'zh' ? '初始资金' : 'Initial Cash'}</span>
                                <span className="text-white font-mono font-medium">{strategy.initial_cash.toLocaleString()}</span>
                            </div>
                            <div className="flex flex-col">
                                <span className="text-white/40 text-xs">{language === 'zh' ? '动态平衡' : 'Rebalance'}</span>
                                <span className={`font-medium ${strategy.enable_rebalance ? 'text-green-400' : 'text-white/60'}`}>
                                    {strategy.enable_rebalance ? (language === 'zh' ? '开启' : 'ON') : (language === 'zh' ? '关闭' : 'OFF')}
                                </span>
                            </div>
                            <div className="flex flex-col">
                                <span className="text-white/40 text-xs">{language === 'zh' ? '买/卖阈值' : 'Buy/Sell Threshold'}</span>
                                <span className="text-white font-mono font-medium">
                                    {strategy.buy_threshold_pct}%|{strategy.sell_threshold_pct}%
                                </span>
                            </div>
                            <div className="flex flex-col">
                                <span className="text-white/40 text-xs">{language === 'zh' ? '仓位范围' : 'Position Range'}</span>
                                <span className="text-white font-mono font-medium">
                                    {Math.round(strategy.min_position_pct * 100)}%-{Math.round(strategy.max_position_pct * 100)}%
                                </span>
                            </div>
                        </div>
                    ) : (
                        <div className="flex-1 flex flex-col items-center justify-center text-center py-2">
                            <p className="text-white/40 text-sm mb-3">
                                {language === 'zh' ? '未配置策略参数' : 'No strategy configured'}
                            </p>
                            <button 
                                onClick={() => setIsModalOpen(true)}
                                className="px-4 py-1.5 rounded-full bg-primary/10 text-primary text-xs font-bold hover:bg-primary/20 transition-colors"
                            >
                                {language === 'zh' ? '立即配置' : 'Configure Now'}
                            </button>
                        </div>
                    )}
                </div>
            </div>

            <AddStrategyModal
                isOpen={isModalOpen}
                onClose={() => setIsModalOpen(false)}
                onSuccess={handleSuccess}
                initialData={strategy || {}}
                uniqueKey={uniqueKey}
            />
        </>
    );
};

export default StrategyCard;
