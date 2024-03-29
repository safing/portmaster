import { Pipe, PipeTransform } from "@angular/core";

@Pipe({
  name: 'toSeconds',
  pure: true,
})
export class ToSecondsPipe implements PipeTransform {
  transform(value: Date | string, ...args: any[]) {
    if (value === null || value === undefined) {
      return NaN
    }

    if (typeof value === 'string') {
      value = new Date(value);
    }

    return Math.floor(value.getTime() / 1000)
  }
}
