import { CdkScrollable } from '@angular/cdk/scrolling';
import { Component, DestroyRef, OnInit, TrackByFunction, ViewChild, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router } from '@angular/router';
import { DebugAPI } from '@safing/portmaster-api';
import { ConfirmDialogConfig, SfngDialogService } from '@safing/ui';
import { BehaviorSubject, Observable, of } from 'rxjs';
import { debounceTime, mergeMap } from 'rxjs/operators';
import { SessionDataService, StatusService } from 'src/app/services';
import { Issue, SupportHubService } from 'src/app/services/supporthub.service';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { fadeInAnimation, fadeInListAnimation, moveInOutAnimation } from 'src/app/shared/animations';
import { FuzzySearchService } from 'src/app/shared/fuzzySearch';
import { SupportPage, supportTypes } from '../pages';
import { INTEGRATION_SERVICE } from 'src/app/integration';
import { SupportProgressDialogComponent, TicketData, TicketInfo } from '../progress-dialog';

@Component({
  templateUrl: './support-form.html',
  styleUrls: ['./support-form.scss'],
  animations: [fadeInAnimation, moveInOutAnimation, fadeInListAnimation]
})
export class SupportFormComponent implements OnInit {
  private readonly destroyRef = inject(DestroyRef);
  private readonly search$ = new BehaviorSubject<string>('');
  private readonly integration = inject(INTEGRATION_SERVICE);

  page: SupportPage | null = null;

  debugData: string = '';
  title: string = '';
  form: { [key: string]: string } = {}
  selectedRepo: string = '';
  haveGhAccount = false;
  version: string = '';
  buildDate: string = '';
  titleMissing = false;

  relatedIssues: Issue[] = [];
  allIssues: Issue[] = [];
  repos: { [repo: string]: string } = {};

  @ViewChild(CdkScrollable)
  scrollContainer: CdkScrollable | null = null;

  trackIssue: TrackByFunction<Issue> = (_: number, issue: Issue) => issue.url;

  constructor(
    private route: ActivatedRoute,
    private router: Router,
    private uai: ActionIndicatorService,
    private debugapi: DebugAPI,
    private statusService: StatusService,
    private dialog: SfngDialogService,
    private supporthub: SupportHubService,
    private searchService: FuzzySearchService,
    private sessionService: SessionDataService,
  ) { }

  ngOnInit() {
    this.supporthub.loadIssues().subscribe(issues => {
      issues = issues.reverse();
      this.allIssues = issues;
      this.relatedIssues = issues;
    })

    this.search$.pipe(
      takeUntilDestroyed(this.destroyRef),
      debounceTime(200),
    )
      .subscribe((text) => {
        this.relatedIssues = this.searchService.searchList(this.allIssues, text, {
          disableHighlight: true,
          shouldSort: true,
          isCaseSensitive: false,
          minMatchCharLength: 4,
          keys: [
            'title',
            'body',
          ],
        }).map(res => res.item)
      })

    this.statusService.getVersions()
      .subscribe(status => {
        this.version = status.Core.Version;
        this.buildDate = status.Core.BuildTime;
      })

    this.route.paramMap
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(params => {
        const id = params.get("id")
        for (let pIdx = 0; pIdx < supportTypes.length; pIdx++) {
          const pageSection = supportTypes[pIdx];
          const page = pageSection.choices.find(choice => choice.type !== 'link' && choice.id === id);
          if (!!page) {
            this.page = page as SupportPage;
            break;
          }
        }

        if (!this.page) {
          this.router.navigate(['..']);
          return;
        }
        this.title = '';
        this.form = {};
        this.selectedRepo = 'portmaster';
        this.debugData = '';
        this.repos = {};
        this.page.sections.forEach(section => this.form[section.title] = '');
        this.page.repositories?.forEach(repo => this.repos[repo.repo] = repo.name)

        // try to restore from session service
        this.sessionService.restore(this.page.id, this);

        if (this.page.includeDebugData) {
          this.debugapi.getCoreDebugInfo('github')
            .subscribe({
              next: data => this.debugData = data,
              error: err => this.uai.error('获取调试数据失败', this.uai.getErrorMessgae(err))
            })
        }
      })
  }

  onModelChange() {
    if (!this.page) {
      return;
    }
    this.sessionService.save(this.page.id, this, ['title', 'form', 'selectedRepo', 'haveGhAccount']);
  }

  selectRepo(repo: string) {
    this.selectedRepo = repo;
    this.onModelChange();
  }

  searchIssues(text: string) {
    this.onModelChange();
    this.search$.next(text);
  }

  copyToClipboard(what: string) {
    this.integration.writeToClipboard(what)
      .then(() => this.uai.success("已复制到剪贴板"))
      .catch(() => this.uai.error('复制到剪贴板失败'));
  }

  validate(): boolean {
    this.titleMissing = this.title === '';
    const valid = !this.titleMissing;
    if (!valid) {
      this.scrollContainer?.scrollTo({ top: 0, behavior: 'smooth' })
    }
    return valid;
  }

  createIssue(type: 'github' | 'private', genUrl?: boolean, email?: string) {
    const ticketData: TicketData = {
      repo: this.selectedRepo || '',
      title: this.title,
      debugInfo: this.debugData,
      sections: this.page?.sections.map(section => ({
        title: section.title,
        body: this.form[section.title],
      })) || [],
    }

    let issue: TicketInfo;

    switch (type) {
      case 'github':
        issue = {
          type: 'github',
          generateUrl: genUrl || false,
          preset: this.page!.ghIssuePreset || '',
          ...ticketData
        };

        break;

      case 'private':
        issue = {
          type: 'private',
          email: email,
          ...ticketData
        }

        break;
    }

    SupportProgressDialogComponent.open(this.dialog, issue)
      .subscribe(() => {
        this.sessionService.delete(this.page?.id || '');
      });
  }

  createOnGithub(genUrl?: boolean) {
    if (!this.validate()) {
      return;
    }

    if (genUrl === undefined && this.haveGhAccount) {
      genUrl = true;
    }

    if (genUrl === undefined) {
      this.dialog.confirm({
        canCancel: true,
        caption: '注意',
        header: '在 GitHub 上创建 Issue',
        message: '你可以使用自己的 GitHub 账户轻松创建 issue。也可以匿名创建 GitHub issue，但这样我们将无法与你联系以获取更多信息。',
        buttons: [
          { id: 'createWithout', text: '不使用账户创建', class: 'outline' },
          { id: 'openGithub', text: '使用我的账户' },
        ]
      })
        .onAction('openGithub', () => {
          this.createIssue('github', true)
        })
        .onAction('createWithout', () => {
          this.createIssue('github', false)
        })
      return;
    }
  }

  openIssue(issue: Issue) {
    this.integration.openExternal(issue.url);
  }

  createPrivateTicket() {
    if (!this.validate()) {
      return;
    }

    const opts: ConfirmDialogConfig = {
      caption: '信息',
      canCancel: true,
      header: '我们应如何与你保持联系？',
      message: '请输入你的电子邮件地址，以便我们在问题解决之前与你保持沟通。',
      inputModel: '',
      inputPlaceholder: '电子邮件（可选）',
      inputType: 'text',
      buttons: [
        { id: '', class: 'outline', text: '取消' },
        { id: 'create', text: '创建工单' },
      ],
    }
    this.dialog.confirm(opts)
      .onAction('create', () => {
        this.createIssue('private', undefined, opts.inputModel);
      });
  }

}
