import { Pipe, PipeTransform } from '@angular/core';

@Pipe({
  name: 'timeAgo',
  pure: true
})
export class TimeAgoPipe implements PipeTransform {
  transform(value: number | Date | string, ticker?: any): string {
    return timeAgo(value);
  }
}

export const timeCeilings = [
  { ceiling: 1, text: "" },
  { ceiling: 60, text: "秒" },
  { ceiling: 3600, text: "分钟" },
  { ceiling: 86400, text: "小时" },
  { ceiling: 2629744, text: "天" },
  { ceiling: 31556926, text: "个月" },
  { ceiling: Infinity, text: "年" }
]

export function timeAgo(value: number | Date | string) {
  if (typeof value === 'string') {
    value = new Date(value)
  }

  if (value instanceof Date) {
    value = value.valueOf() / 1000;
  }

  let suffix = '前'

  let diffInSeconds = Math.floor(((new Date()).valueOf() - (value * 1000)) / 1000);
  if (diffInSeconds < 0) {
    diffInSeconds = diffInSeconds * -1;
    suffix = ''
  }

  for (let i = timeCeilings.length - 1; i >= 0; i--) {
    const f = timeCeilings[i];
    let n = Math.floor(diffInSeconds / f.ceiling);
    if (n > 0) {
      if (i < 1) {
        return `< 1 分钟` + suffix;
      }
      let text = timeCeilings[i + 1].text;
      return `${n} ${text}` + suffix
    }
  }

  return "< 1 分钟" + suffix // actually just now (diffInSeconds == 0)
}
