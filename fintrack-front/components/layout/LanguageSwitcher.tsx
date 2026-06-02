import React, { useState } from 'react';
import { useLanguage, Language } from '../../contexts/LanguageContext';

interface LanguageSwitcherProps {
  variant?: 'default' | 'embedded' | 'sidebar';
}

const LanguageSwitcher: React.FC<LanguageSwitcherProps> = ({ variant = 'default' }) => {
  const { language, setLanguage, t } = useLanguage();
  const [isOpen, setIsOpen] = useState(false);

  const languages = [
    { code: 'en' as Language, name: 'English', flag: '🇺🇸' },
    { code: 'zh' as Language, name: '中文', flag: '🇨🇳' },
  ];

  const currentLanguage = languages.find(lang => lang.code === language);
  const isEmbedded = variant === 'embedded';
  const isSidebar = variant === 'sidebar';

  const handleLanguageChange = (langCode: Language) => {
    setLanguage(langCode);
    setIsOpen(false);
  };

  return (
    <div className={`relative ${isOpen ? 'z-[90]' : 'z-0'}`}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className={`w-full transition-colors ${isEmbedded
          ? 'flex items-center justify-between gap-3 rounded-xl border border-white/8 bg-black/10 px-3 py-2.5 hover:bg-white/6'
          : isSidebar
          ? 'flex items-center gap-3 rounded-lg px-3 py-2 text-white/80 hover:bg-white/10 hover:text-white'
          : 'flex items-center space-x-2 rounded-lg border border-white/10 bg-white/5 px-3 py-2 hover:bg-white/10'
        }`}
        aria-label={t('language.switch')}
      >
        {isEmbedded ? (
          <>
            <div className="flex min-w-0 items-center gap-3">
              <span className="flex h-9 w-9 items-center justify-center rounded-full bg-white/7 text-base">
                {currentLanguage?.flag}
              </span>
              <div className="min-w-0 text-left">
                <p className="text-[10px] font-medium uppercase tracking-[0.22em] text-white/34">Language</p>
                <p className="mt-1 text-sm font-medium text-white/88">{currentLanguage?.name}</p>
              </div>
            </div>
            <span className={`material-symbols-outlined text-sm text-white/55 transition-transform ${isOpen ? 'rotate-180' : ''}`}>
              expand_more
            </span>
          </>
        ) : isSidebar ? (
          <>
            <span className="material-symbols-outlined text-[20px]">language</span>
            <span className="text-sm font-medium leading-normal text-white">{currentLanguage?.name}</span>
            <span className={`material-symbols-outlined ml-auto text-sm transition-transform ${isOpen ? 'rotate-180' : ''}`}>
              expand_more
            </span>
          </>
        ) : (
          <>
            <span className="text-lg">{currentLanguage?.flag}</span>
            <span className="text-sm font-medium text-white">
              {currentLanguage?.name}
            </span>
            <span className={`material-symbols-outlined text-sm transition-transform ${isOpen ? 'rotate-180' : ''}`}>
              expand_more
            </span>
          </>
        )}
      </button>

      {isOpen && (
        <>
          {/* Backdrop */}
          <div 
            className="fixed inset-0 z-[80]" 
            onClick={() => setIsOpen(false)}
          />
          
          {/* Dropdown */}
          <div className={`absolute top-full left-0 z-[91] mt-2 w-full min-w-[140px] overflow-hidden border border-white/10 shadow-lg ${isEmbedded
            ? 'rounded-xl bg-[#15271e] shadow-[0_18px_40px_rgba(0,0,0,0.28)]'
            : isSidebar
            ? 'rounded-xl bg-[#15271e] shadow-[0_18px_40px_rgba(0,0,0,0.28)]'
            : 'rounded-lg bg-[#1a2f23]'
          }`}>
            {languages.map((lang) => (
              <button
                key={lang.code}
                onClick={() => handleLanguageChange(lang.code)}
                className={`w-full text-left transition-colors ${isEmbedded || isSidebar ? 'flex items-center gap-3 px-3 py-2.5 hover:bg-white/6' : 'flex items-center space-x-3 px-3 py-2 hover:bg-white/5'} first:rounded-t-lg last:rounded-b-lg ${
                  language === lang.code ? 'bg-primary/20 text-primary' : 'text-white'
                }`}
              >
                <span className="text-lg">{isSidebar ? <span className="material-symbols-outlined text-[18px]">language</span> : lang.flag}</span>
                <span className="text-sm font-medium">{lang.name}</span>
                {language === lang.code && (
                  <span className="material-symbols-outlined text-sm ml-auto">
                    check
                  </span>
                )}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
};

export default LanguageSwitcher;
