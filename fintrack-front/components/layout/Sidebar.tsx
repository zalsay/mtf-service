
import React, { useState, useEffect } from 'react';
import { View } from '../../types';
import { NAVIGATION_ITEMS } from '../../constants';
import { useLanguage } from '../../contexts/LanguageContext';
import { authAPI } from '../../services/apiService';

const VIP_GOLD_GRADIENT = 'linear-gradient(135deg, #FFF1B8 0%, #FCD34D 36%, #F59E0B 68%, #F97316 100%)';

interface SidebarProps {
  currentView: View;
  setCurrentView: (view: View) => void;
  onLogout: () => void;
  isDemoMode?: boolean;
  isAdmin?: boolean;
}

const Sidebar: React.FC<SidebarProps> = ({ currentView, setCurrentView, onLogout, isDemoMode = false, isAdmin = false }) => {
    const { t } = useLanguage();
    const [userEmail, setUserEmail] = useState<string>('');
    const [membershipLevel, setMembershipLevel] = useState<number>(0);

    useEffect(() => {
        if (isDemoMode) {
            setUserEmail('Demo User');
            return;
        }
        
        const fetchProfile = async () => {
            try {
                const user = await authAPI.getProfile();
                setUserEmail(user.email);
                setMembershipLevel(user.membership_level ?? 0);
            } catch (error) {
                console.error('Failed to fetch user profile', error);
            }
        };
        fetchProfile();
    }, [isDemoMode]);

    const membershipLabel = (() => {
        switch (membershipLevel) {
            case 3: return 'UVIP';
            case 2: return 'SVIP';
            case 1: return 'VIP';
            default: return t('sidebar.member.standard');
        }
    })();

    const userDisplay = userEmail || t('sidebar.loading');
    const userInitial = userDisplay.trim().charAt(0).toUpperCase() || 'U';
    const isVIP = membershipLevel > 0;
    const navigationItems = NAVIGATION_ITEMS.filter(item => !isDemoMode || item.id !== 'mtfAgent');
    
    return (
        <aside className="w-64 shrink-0 bg-background-dark p-4 flex-col justify-between hidden lg:flex sticky top-0 h-screen">
            <div className="flex flex-col gap-8 flex-1 overflow-y-auto no-scrollbar">
                <div className="flex items-center gap-2 px-2">
                    <span className="material-symbols-outlined text-primary text-3xl">trending_up</span>
                    <h1 className="text-white text-xl font-bold">{t('sidebar.title')}</h1>
                </div>
                <div className="flex flex-col gap-4">
                    <div className="px-2">
                        <div className="rounded-2xl border border-white/10 bg-gradient-to-br from-white/10 via-white/6 to-transparent p-4 shadow-[0_14px_36px_rgba(0,0,0,0.18)] backdrop-blur-sm">
                            <div className="flex items-center gap-3">
                                <div
                                    className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl text-sm font-black shadow-[inset_0_1px_0_rgba(255,255,255,0.12)] ${
                                        isVIP
                                            ? 'border border-amber-100/45 text-[#171107] shadow-[0_12px_26px_rgba(245,158,11,0.22),inset_0_1px_0_rgba(255,255,255,0.35)]'
                                            : 'border border-primary/15 bg-primary/12 text-primary'
                                    }`}
                                    style={isVIP ? { background: VIP_GOLD_GRADIENT } : undefined}
                                >
                                    {userInitial}
                                </div>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate text-sm font-medium leading-normal text-white/88">{userDisplay}</p>
                                    <p
                                        className={`mt-1 flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-[0.16em] ${
                                            isVIP ? 'text-amber-200' : 'text-white/42'
                                        }`}
                                    >
                                        {isVIP && <span className="material-symbols-outlined text-[14px] leading-none">workspace_premium</span>}
                                        <span>{membershipLabel}</span>
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>

                    <nav className="flex flex-col gap-2">
                        {navigationItems.map(item => (
                            <a
                                key={item.id}
                                className={`flex items-center gap-3 px-3 py-2 rounded-lg transition-colors cursor-pointer ${
                                    currentView === item.id 
                                    ? 'bg-primary/20 text-primary' 
                                    : 'text-white/80 hover:bg-white/10 hover:text-white'
                                }`}
                                onClick={() => setCurrentView(item.id)}
                            >
                                <span className="material-symbols-outlined" style={{ fontVariationSettings: currentView === item.id ? "'FILL' 1" : "" }}>
                                    {item.icon}
                                </span>
                                <p className="text-sm font-medium leading-normal">{t(`nav.${item.id}`)}</p>
                            </a>
                        ))}
                        {isAdmin && (
                            <a
                                className={`flex items-center gap-3 px-3 py-2 rounded-lg transition-colors cursor-pointer ${
                                    currentView === 'admin'
                                    ? 'bg-primary/20 text-primary'
                                    : 'text-white/80 hover:bg-white/10 hover:text-white'
                                }`}
                                onClick={() => setCurrentView('admin')}
                            >
                                <span className="material-symbols-outlined" style={{ fontVariationSettings: currentView === 'admin' ? "'FILL' 1" : "" }}>
                                    admin_panel_settings
                                </span>
                                <p className="text-sm font-medium leading-normal">{t('nav.admin')}</p>
                            </a>
                        )}
                    </nav>
                </div>
            </div>
            <div className="flex flex-col gap-1 shrink-0 mt-2">
                <a
                    onClick={() => setCurrentView('settings')}
                    className={`flex items-center gap-3 px-3 py-2 rounded-lg transition-colors cursor-pointer ${
                        currentView === 'settings'
                            ? 'bg-primary/20 text-primary'
                            : 'text-white/80 hover:bg-white/10 hover:text-white'
                    }`}
                >
                    <span className="material-symbols-outlined">settings</span>
                    <p className="text-sm font-medium leading-normal">{t('common.settings') || 'Settings'}</p>
                </a>
            </div>
        </aside>
    );
};

export default Sidebar;
