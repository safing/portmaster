import { Component, DestroyRef, OnInit, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { BehaviorSubject, combineLatest, debounceTime } from 'rxjs';
import { Issue, SupportHubService } from 'src/app/services';
import { fadeInAnimation, fadeInListAnimation } from 'src/app/shared/animations';
import { FuzzySearchService } from 'src/app/shared/fuzzySearch';
import { SupportType, supportTypes } from './pages';
import { INTEGRATION_SERVICE } from 'src/app/integration';

@Component({
  templateUrl: './support.html',
  styleUrls: ['./support.scss'],
  animations: [
    fadeInListAnimation,
    fadeInAnimation,
  ]
})
export class SupportPageComponent implements OnInit {
  // make supportTypes available in the page template.
  readonly supportTypes = supportTypes;

  private readonly destroyRef = inject(DestroyRef);
  private readonly integration = inject(INTEGRATION_SERVICE);

  /** @private The current search term for the FAQ entries. */
  searchFaqs = new BehaviorSubject<string>('');

  searchTerm: string = '';

  /** A list of all faq entries loaded from the Support Hub */
  allFaqEntries: Issue<Date>[] = [];

  /** A list of faq entries to show */
  faqEntries: Issue<Date>[] = [];

  constructor(
    private router: Router,
    private searchService: FuzzySearchService,
    private supportHub: SupportHubService,
  ) { }

  ngOnInit(): void {
    combineLatest([
      this.searchFaqs,
      this.supportHub.loadIssues()
    ])
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        debounceTime(200),
      )
      .subscribe(([searchTerm, allFaqEntries]) => {
        this.allFaqEntries = allFaqEntries
          .filter(issue => issue.labels?.includes("faq"))
          .map(issue => {
            return {
              ...issue,

              title: issue.title.replace("FAQ: ", "")
            }
          })

        if (searchTerm === '') {
          this.faqEntries = [
            ...this.allFaqEntries
          ]

          return;
        }

        this.faqEntries = this.searchService.searchList(this.allFaqEntries, searchTerm, {
          disableHighlight: true,
          shouldSort: true,
          isCaseSensitive: false,
          minMatchCharLength: 3,
          keys: [
            'title',
            'body',
          ],
        }).map(res => res.item)
      })
  }

  openIssue(issue: Issue<any>) {
    this.integration.openExternal(issue.url);
  }

  openPage(item: SupportType) {
    if (item.type === 'link') {
      this.integration.openExternal(item.url);
      return;
    }

    this.router.navigate(['/support', item.id]);
  }
}

