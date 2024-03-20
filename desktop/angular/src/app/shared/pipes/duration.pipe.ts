import { Pipe, PipeTransform } from "@angular/core";

const millisecond = 1;
const second = 1000 * millisecond;
const minute = 60 * second;
const hour = 60 * minute;
const day = 24 * hour;

export function formatDuration(millis: number, skipDays = false, skipMillis = false): string {
  const sign = millis < 0 ? '-' : '';
  let val = Math.abs(millis);
  let str = '';

  if (millis === 0) {
    return '0';
  }

  if (!skipDays) {
    const days = Math.floor(val / day)
    if (days > 0) {
      str += days.toString() + 'd ';
      val -= days * day;
    }
  }

  const hours = Math.floor(val / hour);
  if (hours > 0) {
    str += hours.toString() + 'h ';
    val -= hours * hour;
  }

  const minutes = Math.floor(val / minute);
  if (minutes > 0) {
    str += minutes.toString() + 'm ';
    val -= minutes * minute;
  }

  const seconds = Math.floor(val / second);
  if (seconds > 0) {
    str += seconds.toString() + 's ';
    val -= seconds * second;
  }

  if (!skipMillis) {
    const ms = Math.floor(val / millisecond)
    if (ms > 0) {
      str += ms.toString() + 'ms '
      val -= ms * millisecond
    }
  }

  if (str.endsWith("")) {
    str = str.substring(0, str.length - 1)
  }

  return sign + str;
}

@Pipe({
  name: 'duration',
  pure: true
})
export class DurationPipe implements PipeTransform {
  transform(value: number | [string, string] | [Date, Date] | [number, number], ...args: any[]) {
    if (Array.isArray(value)) {
      let firstNum: number;
      let secondNum: number;

      let [first, second] = value;
      if (first instanceof Date || typeof first === 'string') {
        first = new Date(first)
        firstNum = first.getTime()
      } else {
        firstNum = first
      }
      if (second instanceof Date || typeof second === 'string') {
        second = new Date(second);
        secondNum = second.getTime()
      } else {
        secondNum = second
      }

      if (secondNum < firstNum) {
        const t = firstNum;
        firstNum = secondNum
        secondNum = t
      }

      value = secondNum - firstNum
    }

    if (value < second) {

    }

    const result = formatDuration(value);
    if (result === '0') {
      return '< 1s'
    }

    return result
  }
}
