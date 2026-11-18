import { clsx, type ClassValue } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

/**
 * tailwind-merge 默认只认识 Tailwind 内置字号；WinUI 语义字阶（text-caption /
 * text-body / text-body-strong / text-subtitle / text-title / text-title-large /
 * text-display）是 main.css 里自定义的 utility，需要显式登记为 font-size 组，
 * 否则会和 `text-foreground` 等颜色类误判冲突而被合并掉。
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': [
        { text: ['caption', 'body', 'body-strong', 'subtitle', 'title', 'title-large', 'display'] },
      ],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
