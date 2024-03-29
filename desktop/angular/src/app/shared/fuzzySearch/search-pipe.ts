import { Pipe, PipeTransform } from '@angular/core';
import { FuseResult, FuseSearchOpts, FuzzySearchService } from './fuse.service';


@Pipe({
  name: 'fuzzySearch',
})
export class FuzzySearchPipe implements PipeTransform {
  constructor(
    private FusejsService: FuzzySearchService
  ) { }

  transform<T extends object>(elements: Array<T>,
    searchTerms: string,
    options: FuseSearchOpts<T> = {}): Array<FuseResult<T>> {

    return this.FusejsService.searchList(elements, searchTerms, options);
  }
}
