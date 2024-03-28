import { DecimalPipe } from "@angular/common";
import { Pipe, PipeTransform } from "@angular/core";

@Pipe({
  pure: true,
  name: 'bytes',
})
export class BytesPipe implements PipeTransform {
  transform(value: any, decimal: string = '1.0-2', ...args: any[]) {
    value = +value; // convert to number

    const ceilings = [
      'B',
      'kB',
      'MB',
      'GB',
      'TB'
    ]

    let idx = 0;
    while (value > 1024 && idx < ceilings.length - 1) {
      value = value / 1024;
      idx++
    }

    return (new DecimalPipe('en-US')).transform(value, decimal) + ' ' + ceilings[idx];
  }
}
