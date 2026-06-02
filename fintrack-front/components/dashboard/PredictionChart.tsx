import React, { useState, useRef } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { getChartColor } from '../../utils/colorUtils';
import { PREDICTION_CHART_TEXT } from '../common/chartTextStyles';

interface ChartProps {
    change: number;
    chartData?: {
        dates: string[];
        actuals: number[];
        predictions: number[];
    };
    currentPrice?: number;
    startPrice?: number;
}

const PredictionChart: React.FC<ChartProps> = ({ change, chartData, currentPrice, startPrice }) => {
    const { language, t } = useLanguage();
    const isPositive = change >= 0;
    const color = getChartColor(isPositive, language);
    const [hoverIndex, setHoverIndex] = useState<number | null>(null);
    const containerRef = useRef<HTMLDivElement>(null);
    
    if (chartData && (chartData.actuals.length > 0 || chartData.predictions.length > 0)) {
        const { actuals, predictions, dates } = chartData;
        const allValues = [...actuals, ...predictions].filter(v => v !== null && v !== undefined);
        
        if (allValues.length > 0) {
            const min = Math.min(...allValues);
            const max = Math.max(...allValues);
            const range = max - min || 1;
            const width = 478;
            const height = 150;
            const padding = 20;
            const chartHeight = height - padding; // Reserve space for dates
            const axisLabelFontSize = PREDICTION_CHART_TEXT.axisFontSize;
            const cornerLabelFontSize = PREDICTION_CHART_TEXT.cornerFontSize;
            const tooltipFontSize = PREDICTION_CHART_TEXT.tooltipFontSize;
            const tooltipMetaFontSize = PREDICTION_CHART_TEXT.tooltipMetaFontSize;
            
            // Assume both arrays align with the same X axis (dates)
            const count = Math.max(actuals.length, predictions.length);
            const stepX = count > 1 ? width / (count - 1) : 0;

            const generatePath = (data: number[]) => {
                if (!data || data.length === 0) return "";
                
                // Helper to get point coordinates
                const getPoint = (i: number) => {
                    const x = i * stepX;
                    const y = chartHeight - ((data[i] - min) / range) * (chartHeight - 20) - 10;
                    return [x, y];
                };

                // Simple smoothing strategy (Catmull-Rom to Bezier)
                // For a point P[i], control points depends on P[i-1] and P[i+1]
                const smoothing = 0.2; // 0 to 1
                
                // Helper to calculate control point
                const line = (pointA: number[], pointB: number[]) => {
                    const lengthX = pointB[0] - pointA[0];
                    const lengthY = pointB[1] - pointA[1];
                    return {
                        length: Math.sqrt(Math.pow(lengthX, 2) + Math.pow(lengthY, 2)),
                        angle: Math.atan2(lengthY, lengthX)
                    };
                };

                const controlPoint = (current: number[], previous: number[], next: number[], reverse?: boolean) => {
                    const p = previous || current;
                    const n = next || current;
                    const o = line(p, n);
                    const angle = o.angle + (reverse ? Math.PI : 0);
                    const length = o.length * smoothing;
                    const x = current[0] + Math.cos(angle) * length;
                    const y = current[1] + Math.sin(angle) * length;
                    return [x, y];
                };

                const points = data.map((_, i) => getPoint(i));
                
                return points.reduce((acc, point, i, a) => {
                    if (i === 0) return `M ${point[0]},${point[1]}`;
                    
                    const [cpsX, cpsY] = controlPoint(a[i - 1], a[i - 2], point);
                    const [cpeX, cpeY] = controlPoint(point, a[i - 1], a[i + 1], true);
                    
                    return `${acc} C ${cpsX},${cpsY} ${cpeX},${cpeY} ${point[0]},${point[1]}`;
                }, "");
            };

            const actualsPath = generatePath(actuals);
            const predictionsPath = generatePath(predictions);

            // Generate Date Labels (Start, Middle, End) -> (Start, 25%, 50%, 75%, End)
            const dateLabels = [];
            const labelCount = 5; // Target number of labels
            if (dates.length > 0) {
                 const step = Math.max(1, Math.floor((dates.length - 1) / (labelCount - 1)));
                 
                 for (let i = 0; i < labelCount; i++) {
                     let idx = i * step;
                     // Adjust last index to be exactly the end
                     if (i === labelCount - 1) idx = dates.length - 1;
                     // Prevent index out of bounds
                     if (idx >= dates.length) break;
                     
                     // Skip duplicates if any (e.g. short arrays)
                     if (i > 0 && idx === 0) continue;
                     
                     const x = idx * stepX;
                     let anchor = "middle";
                     if (i === 0) anchor = "start";
                     else if (idx === dates.length - 1) anchor = "end";

                     dateLabels.push(
                        <text key={i} x={x} y={height - 2} fill="#9CA3AF" fontSize={axisLabelFontSize} textAnchor={anchor}>{dates[idx]}</text>
                     );
                 }
            }

            const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
                if (!containerRef.current || count <= 1) return;
                const rect = containerRef.current.getBoundingClientRect();
                const x = e.clientX - rect.left;
                const idx = Math.min(Math.max(0, Math.round((x / rect.width) * (count - 1))), count - 1);
                setHoverIndex(idx);
            };

            const handleMouseLeave = () => {
                setHoverIndex(null);
            };

            const getY = (val: number) => chartHeight - ((val - min) / range) * (chartHeight - 20) - 10;

            const actualLabel = t('prediction.actual');
            const predLabel = t('prediction.pred');

            return (
                <div ref={containerRef} className="relative w-full h-full" onMouseMove={handleMouseMove} onMouseLeave={handleMouseLeave}>
                    <svg fill="none" height="100%" preserveAspectRatio="none" viewBox="-5 0 490 150" width="100%" xmlns="http://www.w3.org/2000/svg">
                        {/* Actuals Line */}
                        <path d={actualsPath} stroke={color} strokeLinecap="round" strokeWidth="1" fill="none"></path>
                        {/* Predictions Line - Dashed, Theme Color */}
                        <path d={predictionsPath} stroke="currentColor" strokeLinecap="round" strokeWidth="1" fill="none" className="text-primary stroke-current"></path>
                        
                        {/* Area fill for actuals */}
                        {actualsPath && (
                            <path d={`${actualsPath} V${chartHeight} H0 Z`} fill={`url(#paint_dynamic_${isPositive ? 'positive' : 'negative'})`} stroke="none"></path>
                        )}

                        {/* Hover Line and Dots */}
                        {hoverIndex !== null && (
                            <>
                                <line 
                                    x1={hoverIndex * stepX} 
                                    y1={0} 
                                    x2={hoverIndex * stepX} 
                                    y2={chartHeight} 
                                    stroke="#ffffff" 
                                    strokeOpacity="0.2" 
                                    strokeWidth="1" 
                                    strokeDasharray="2 2"
                                />
                                {actuals[hoverIndex] !== undefined && actuals[hoverIndex] !== null && (
                                    <circle cx={hoverIndex * stepX} cy={getY(actuals[hoverIndex])} r="3" fill={color} stroke="#1f2937" strokeWidth="1" />
                                )}
                                {predictions[hoverIndex] !== undefined && predictions[hoverIndex] !== null && (
                                    <circle cx={hoverIndex * stepX} cy={getY(predictions[hoverIndex])} r="3" fill="currentColor" className="text-primary" stroke="#1f2937" strokeWidth="1" />
                                )}
                            </>
                        )}

                        {/* Date Labels */}
                        {dateLabels}
                        
                        <defs>
                            <linearGradient gradientUnits="userSpaceOnUse" id={`paint_dynamic_${isPositive ? 'positive' : 'negative'}`} x1="0" y1="0" x2="0" y2={chartHeight}>
                                <stop stopColor={color} stopOpacity="0.5"></stop>
                                <stop offset="1" stopColor={color} stopOpacity="0"></stop>
                            </linearGradient>
                        </defs>
                    </svg>

                    {/* Tooltip */}
                    {hoverIndex !== null && (
                        <div 
                            className="absolute top-0 pointer-events-none bg-gray-900/90 border border-white/10 rounded p-2 shadow-xl backdrop-blur-sm z-10"
                            style={{ 
                                left: `${(hoverIndex / (count - 1)) * 100}%`, 
                                transform: `translateX(${hoverIndex > count / 2 ? '-100%' : '0%'}) translateY(-100%)`, // Flip side if on right
                                marginTop: '-10px',
                                marginLeft: hoverIndex > count / 2 ? '-10px' : '10px',
                                fontSize: `${tooltipFontSize}px`,
                            }}
                        >
                            <div className="text-white/60 mb-1" style={{ fontSize: `${tooltipMetaFontSize}px` }}>{dates[hoverIndex]}</div>
                            {actuals[hoverIndex] !== undefined && actuals[hoverIndex] !== null && (
                                <div className="flex items-center gap-2">
                                    <div className="w-2 h-2 rounded-full" style={{ backgroundColor: color }}></div>
                                    <span className="text-white">{actualLabel}: {actuals[hoverIndex].toFixed(2)}</span>
                                </div>
                            )}
                            {predictions[hoverIndex] !== undefined && predictions[hoverIndex] !== null && (
                                <div className="flex items-center gap-2">
                                    <div className="w-2 h-2 rounded-full bg-primary"></div>
                                    <span className="text-primary font-medium">{predLabel}: {predictions[hoverIndex].toFixed(2)}</span>
                                </div>
                            )}
                        </div>
                    )}

                    {/* Start Price (Top Left) */}
                    {startPrice !== undefined && (
                        <div className="absolute top-2 left-2 bg-black/30 backdrop-blur-sm px-2 py-1 rounded text-white/70 font-semibold" style={{ fontSize: `${cornerLabelFontSize}px` }}>
                            {startPrice.toFixed(2)}
                        </div>
                    )}

                    {/* Current Price (Top Right) */}
                    {currentPrice !== undefined && (
                        <div className="absolute top-2 right-2 bg-black/30 backdrop-blur-sm px-2 py-1 rounded text-white font-bold" style={{ fontSize: `${cornerLabelFontSize}px` }}>
                            {currentPrice.toFixed(2)}
                        </div>
                    )}
                </div>
            );
        }
    }

    // Static paths for visual representation (Fallback)
    const paths = {
      positive1: "M0 109C18.15 109 18.15 21 36.3 21C54.46 21 54.46 41 72.6 41C90.77 41 90.77 93 108.9 93C127.07 93 127.07 33 145.2 33C163.38 33 163.38 101 181.5 101C199.69 101 199.69 61 217.8 61C236 61 236 45 254.1 45C272.3 45 272.3 121 290.4 121C308.6 121 308.6 149 326.7 149C344.9 149 344.9 1 363 1C381.2 1 381.2 81 399.3 81C417.5 81 417.5 129 435.6 129C453.8 129 453.8 25 472 25",
      positive2: "M0 129C18.15 129 18.15 1 36.3 1C54.46 1 54.46 149 72.6 149C90.77 149 90.77 61 108.9 61C127.07 61 127.07 101 145.2 101C163.38 101 163.38 21 181.5 21C199.69 21 199.69 93 217.8 93C236 93 236 33 254.1 33C272.3 33 272.3 121 290.4 121C308.6 121 308.6 41 326.7 41C344.9 41 344.9 81 363 81C381.2 81 381.2 25 399.3 25C417.5 25 417.5 109 435.6 109C453.8 109 453.8 45 472 45",
      negative: "M0 45C18.15 45 18.15 121 36.3 121C54.46 121 54.46 101 72.6 101C90.77 101 90.77 61 108.9 61C127.07 61 127.07 93 145.2 93C163.38 93 163.38 33 181.5 33C199.69 33 199.69 61 217.8 61C236 61 236 21 254.1 21C272.3 21 272.3 81 290.4 81C308.6 81 308.6 1 326.7 1C344.9 1 344.9 149 363 149C381.2 149 381.2 109 399.3 109C417.5 109 417.5 41 435.6 41C453.8 41 453.8 129 472 129",
    };
    const pathData = isPositive ? (change > 1 ? paths.positive2 : paths.positive1) : paths.negative;

    return (
        <svg fill="none" height="100%" preserveAspectRatio="none" viewBox="-3 0 478 150" width="100%" xmlns="http://www.w3.org/2000/svg">
            <path d={`${pathData}V149H0Z`} fill={`url(#paint_${isPositive ? 'positive' : 'negative'})`}></path>
            <path d={pathData} stroke={color} strokeLinecap="round" strokeWidth="3"></path>
            <defs>
                <linearGradient gradientUnits="userSpaceOnUse" id={`paint_${isPositive ? 'positive' : 'negative'}`} x1="236" x2="236" y1="1" y2="149">
                    <stop stopColor={color} stopOpacity="0.3"></stop>
                    <stop offset="1" stopColor={color} stopOpacity="0"></stop>
                </linearGradient>
            </defs>
        </svg>
    )
};

export default PredictionChart;
