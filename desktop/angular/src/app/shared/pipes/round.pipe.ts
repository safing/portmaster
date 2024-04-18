import { Pipe, PipeTransform } from '@angular/core';

@Pipe({
  name: 'round',
  pure: true,
})
export class RoundPipe implements PipeTransform {
  transform(value: number, roundBy: number) {
    if (isNaN(value)) {
      return NaN
    }

    return Math.floor(value / roundBy) * roundBy
  }
}
