import React from 'react';

interface MarkdownMessageProps {
    content: string;
}

type InlineToken = {
    kind: 'text' | 'code' | 'strong' | 'em' | 'link';
    text: string;
    href?: string;
};

const markdownSignal = /(^|\n)(#{1,6}\s|[-*+]\s|\d+\.\s|>\s|```|\|.+\||\[[^\]]+\]\([^)]+\)|\*\*[^*]+\*\*|`[^`]+`)/;

export const isLikelyMarkdown = (content: string) => markdownSignal.test(content.trim());

const sanitizeHref = (href: string) => {
    const trimmed = href.trim();
    if (/^(https?:|mailto:|tel:)/i.test(trimmed)) return trimmed;
    if (trimmed.startsWith('/')) return trimmed;
    return '#';
};

const tokenizeInline = (text: string): InlineToken[] => {
    const tokens: InlineToken[] = [];
    const pattern = /(\[[^\]]+\]\([^)]+\)|\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g;
    let cursor = 0;
    for (const match of text.matchAll(pattern)) {
        const index = match.index || 0;
        if (index > cursor) tokens.push({ kind: 'text', text: text.slice(cursor, index) });
        const value = match[0];
        if (value.startsWith('[')) {
            const linkMatch = value.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (linkMatch) tokens.push({ kind: 'link', text: linkMatch[1], href: sanitizeHref(linkMatch[2]) });
        } else if (value.startsWith('**')) {
            tokens.push({ kind: 'strong', text: value.slice(2, -2) });
        } else if (value.startsWith('`')) {
            tokens.push({ kind: 'code', text: value.slice(1, -1) });
        } else {
            tokens.push({ kind: 'em', text: value.slice(1, -1) });
        }
        cursor = index + value.length;
    }
    if (cursor < text.length) tokens.push({ kind: 'text', text: text.slice(cursor) });
    return tokens;
};

const renderInline = (text: string) => tokenizeInline(text).map((token, index) => {
    const key = `${token.kind}-${index}`;
    if (token.kind === 'strong') return <strong key={key} className="font-bold text-white">{token.text}</strong>;
    if (token.kind === 'em') return <em key={key} className="text-white/90">{token.text}</em>;
    if (token.kind === 'code') {
        return <code key={key} className="rounded bg-black/30 px-1.5 py-0.5 font-mono text-[0.92em] text-amber-100">{token.text}</code>;
    }
    if (token.kind === 'link') {
        return (
            <a key={key} href={token.href} target="_blank" rel="noreferrer" className="font-semibold text-amber-200 underline decoration-amber-200/40 underline-offset-4 hover:text-amber-100">
                {token.text}
            </a>
        );
    }
    return <React.Fragment key={key}>{token.text}</React.Fragment>;
});

const isTableDivider = (line: string) => /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);

const splitTableRow = (line: string) => line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map(cell => cell.trim());

const renderTable = (lines: string[], key: string) => {
    const rows = lines.filter(line => !isTableDivider(line)).map(splitTableRow);
    const [header, ...body] = rows;
    if (!header || body.length === 0) return null;
    return (
        <div key={key} className="overflow-x-auto rounded-lg border border-white/10">
            <table className="min-w-full border-collapse text-left text-xs">
                <thead className="bg-white/10 text-white">
                    <tr>{header.map((cell, index) => <th key={index} className="border-b border-white/10 px-3 py-2 font-bold">{renderInline(cell)}</th>)}</tr>
                </thead>
                <tbody className="divide-y divide-white/10">
                    {body.map((row, rowIndex) => (
                        <tr key={rowIndex}>
                            {row.map((cell, cellIndex) => <td key={cellIndex} className="px-3 py-2 text-white/78">{renderInline(cell)}</td>)}
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
};

const renderList = (lines: string[], ordered: boolean, key: string) => {
    const Tag = ordered ? 'ol' : 'ul';
    return (
        <Tag key={key} className={`${ordered ? 'list-decimal' : 'list-disc'} space-y-1 pl-5`}>
            {lines.map((line, index) => (
                <li key={index} className="pl-1 text-white/82">
                    {renderInline(line.replace(ordered ? /^\s*\d+\.\s+/ : /^\s*[-*+]\s+/, ''))}
                </li>
            ))}
        </Tag>
    );
};

const renderBlocks = (content: string) => {
    const nodes: React.ReactNode[] = [];
    const lines = content.replace(/\r\n/g, '\n').split('\n');
    for (let index = 0; index < lines.length; index += 1) {
        const line = lines[index];
        if (!line.trim()) continue;
        const codeFence = line.match(/^```(\w+)?\s*$/);
        if (codeFence) {
            const codeLines: string[] = [];
            while (++index < lines.length && !/^```\s*$/.test(lines[index])) codeLines.push(lines[index]);
            nodes.push(<pre key={nodes.length} className="overflow-x-auto rounded-lg bg-black/35 p-3 text-xs leading-5 text-white/80"><code>{codeLines.join('\n')}</code></pre>);
            continue;
        }
        const tableLines = collectWhile(lines, index, item => item.includes('|'));
        if (tableLines.length >= 3 && tableLines.some(isTableDivider)) {
            nodes.push(renderTable(tableLines, String(nodes.length)));
            index += tableLines.length - 1;
            continue;
        }
        const heading = line.match(/^(#{1,6})\s+(.+)$/);
        if (heading) {
            const level = Math.min(heading[1].length, 3);
            const className = level === 1 ? 'text-base font-black text-white' : 'text-sm font-bold text-white';
            nodes.push(React.createElement(`h${level + 2}`, { key: nodes.length, className }, renderInline(heading[2])));
            continue;
        }
        const ordered = /^\s*\d+\.\s+/.test(line);
        if (ordered || /^\s*[-*+]\s+/.test(line)) {
            const listLines = collectWhile(lines, index, item => ordered ? /^\s*\d+\.\s+/.test(item) : /^\s*[-*+]\s+/.test(item));
            nodes.push(renderList(listLines, ordered, String(nodes.length)));
            index += listLines.length - 1;
            continue;
        }
        if (/^\s*>\s+/.test(line)) {
            nodes.push(<blockquote key={nodes.length} className="border-l-2 border-amber-200/70 pl-3 text-white/70">{renderInline(line.replace(/^\s*>\s+/, ''))}</blockquote>);
            continue;
        }
        const paragraphLines = collectWhile(lines, index, item => Boolean(item.trim()) && !isBlockStart(item));
        nodes.push(<p key={nodes.length} className="text-white/82">{renderInline(paragraphLines.join(' '))}</p>);
        index += paragraphLines.length - 1;
    }
    return nodes;
};

const collectWhile = (lines: string[], start: number, predicate: (line: string) => boolean) => {
    const result: string[] = [];
    for (let index = start; index < lines.length && predicate(lines[index]); index += 1) {
        result.push(lines[index]);
    }
    return result;
};

const isBlockStart = (line: string) => (
    /^```/.test(line) ||
    /^(#{1,6})\s+/.test(line) ||
    /^\s*(?:[-*+]|\d+\.)\s+/.test(line) ||
    /^\s*>\s+/.test(line)
);

const MarkdownMessage: React.FC<MarkdownMessageProps> = ({ content }) => (
    <div className="space-y-3 break-words">
        {renderBlocks(content)}
    </div>
);

export default MarkdownMessage;
