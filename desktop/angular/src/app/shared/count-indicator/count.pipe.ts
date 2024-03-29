import { Pipe, PipeTransform } from "@angular/core";

@Pipe({
  name: 'prettyCount',
  pure: true
})
export class PrettyCountPipe implements PipeTransform {
  transform(value: number) {
    if (value > 999) {
      const v = Math.floor(value / 1000);
      if (value === v * 1000) {
        return `${v}k`;
      }
      return `${v}k+`
    }
    return `${value}`
  }
}
