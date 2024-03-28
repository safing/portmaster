import { Injectable } from '@angular/core';
import { deepClone } from '@safing/portmaster-api';
import Fuse from 'fuse.js';

export type FuseResult<T> = Fuse.FuseResult<T & {
  highlighted?: string;
}>;

export interface FuseSearchOpts<T> extends Fuse.IFuseOptions<T> {
  minSearchTermLength?: number;
  maximumScore?: number;
}

@Injectable({
  providedIn: 'root'
})
export class FuzzySearchService {

  readonly defaultOptions: FuseSearchOpts<any> = {
    minMatchCharLength: 2,
    includeMatches: true,
    includeScore: true,
    minSearchTermLength: 3,
  };

  searchList<T extends {}>(list: Array<T>, searchTerms: string, options: FuseSearchOpts<T> & { disableHighlight?: boolean } = {}): Array<FuseResult<T>> {
    const opts: FuseSearchOpts<T> = {
      ...this.defaultOptions,
      ...options,
    }

    let result: FuseResult<T>[] = [];


    if (searchTerms && searchTerms.length >= (opts.minSearchTermLength || 0)) {
      let fuse = new Fuse(list, opts);
      result = fuse.search(searchTerms);

    } else {
      result = list.map((item, index) => ({
        item: item,
        refIndex: index,
        score: 0,
      }))
    }

    if (!!options.disableHighlight) {
      return result;
    }

    return this.handleHighlight(result, options);
  }

  private handleHighlight<T extends {}>(result: FuseResult<T>[], options: FuseSearchOpts<T>): FuseResult<T>[] {
    return result.map(matchObject => {
      matchObject.item = deepClone(matchObject.item);

      if (!matchObject.matches) {
        return matchObject;
      }

      for (let match of matchObject.matches!) {
        const indices = match.indices;

        let highlightOffset: number = 0;

        for (let indice of indices) {
          let initialValue = getFromMatch(matchObject, match);

          const startOffset = indice[0] + highlightOffset;
          const endOffset = indice[1] + highlightOffset + 1;

          if (endOffset - startOffset < 4) {
            continue
          }

          let highlightedTerm = initialValue.substring(startOffset, endOffset);
          let newValue = initialValue.substring(0, startOffset) + '<em class="search-result">' + highlightedTerm + '</em>' + initialValue.substring(endOffset);

          highlightOffset += '<em class="search-result"></em>'.length;

          setOnMatch(matchObject, match, newValue);
        }
      }

      return matchObject;
    });
  }
}

function getFromMatch<T>(result: Fuse.FuseResult<T>, match: Fuse.FuseResultMatch): string {
  if (match.refIndex === undefined) {
    return (result.item as any)[match.key!];
  }
  return (result.item as any)[match.key!][match.refIndex];
}

function setOnMatch<T>(result: Fuse.FuseResult<T>, match: Fuse.FuseResultMatch, value: string) {
  if (match.refIndex === undefined) {
    (result.item as any)[match.key!] = value;
    return;
  }

  (result.item as any)[match.key!][match.refIndex] = value;
}
