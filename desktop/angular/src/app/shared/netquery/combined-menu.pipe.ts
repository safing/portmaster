import { KeyValue } from '@angular/common';
import { Pipe, PipeTransform } from "@angular/core";

interface Model {
  visible: boolean | 'combinedMenu';
}

@Pipe({
  pure: true,
  name: 'combinedMenu'
})
export class CombinedMenuPipe implements PipeTransform {
  transform<T extends Model>(value: KeyValue<any, T | undefined>[], ...args: any[]) {
    return value.filter(entry => entry.value?.visible === 'combinedMenu')
  }
}
