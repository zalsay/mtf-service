import { Language } from '../contexts/LanguageContext';

/**
 * 获取涨跌颜色配置
 * @param isPositive 是否为正值（上涨）
 * @param language 当前语言环境
 * @returns 返回对应的颜色类名和十六进制颜色值
 */
export const getChangeColors = (isPositive: boolean, language: Language) => {
  // 中文环境：涨红跌绿
  // 英文环境：涨绿跌红（国际惯例）
  if (language === 'zh') {
    return {
      textClass: isPositive ? 'text-loss' : 'text-gain', // 涨红跌绿（中国惯例）
      hexColor: isPositive ? '#EF4444' : '#22C55E'       // 红涨绿跌
    };
  } else {
    return {
      textClass: isPositive ? 'text-gain' : 'text-loss', // 涨绿跌红（国际惯例）
      hexColor: isPositive ? '#22C55E' : '#EF4444'       // 绿涨红跌
    };
  }
};

/**
 * 获取图表颜色（用于SVG等）
 * @param isPositive 是否为正值（上涨）
 * @param language 当前语言环境
 * @returns 返回十六进制颜色值
 */
export const getChartColor = (isPositive: boolean, language: Language): string => {
  const { hexColor } = getChangeColors(isPositive, language);
  return hexColor;
};
