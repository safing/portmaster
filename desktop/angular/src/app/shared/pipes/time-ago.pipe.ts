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
  { ceiling: 60, text: "sec" },
  { ceiling: 3600, text: "min" },
  { ceiling: 86400, text: "hour" },
  { ceiling: 2629744, text: "day" },
  { ceiling: 31556926, text: "month" },
  { ceiling: Infinity, text: "year" }
]

export function timeAgo(value: number | Date | string) {
  if (typeof value === 'string') {
    value = new Date(value)
  }

  if (value instanceof Date) {
    value = value.valueOf() / 1000;
  }

  let suffix = 'ago'

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
        return `< 1 min ` + suffix;
      }
      let text = timeCeilings[i + 1].text;
      if (n > 1) {
        text += 's';
      }
      return `${n} ${text} ` + suffix
    }
  }

  return "< 1 min" + suffix // actually just now (diffInSeconds == 0)
}
