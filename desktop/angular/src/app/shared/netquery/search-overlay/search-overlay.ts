import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Inject, OnInit, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { Router } from "@angular/router";
import { SfngDialogRef, SFNG_DIALOG_REF } from "@safing/ui";
import { I18nService } from "src/i18n";
import { objKeys } from "../../utils";
import { NetqueryHelper } from "../connection-helper.service";
import { SfngSearchbarFields } from "../searchbar";
import { createLocalizedFieldTranslation } from "../utils";

@Component({
  selector: 'sfng-netquery-search-overlay',
  templateUrl: './search-overlay.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    NetqueryHelper,
  ],
  styles: [
    `
    :host {
      @apply block;
      width: 700px;
    }

    ::ng-deep sfng-netquery-search-overlay sfng-netquery-searchbar input {
      border: 1px solid theme("colors.gray.200") !important;
    }
    `
  ]
})
export class SfngNetquerySearchOverlayComponent implements OnInit {
  private readonly i18n = inject(I18nService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly cdr = inject(ChangeDetectorRef);

  keyTranslation = createLocalizedFieldTranslation(this.i18n);

  textSearch = '';

  fields: SfngSearchbarFields = {};

  constructor(
    @Inject(SFNG_DIALOG_REF) private dialogRef: SfngDialogRef<any>,
    private router: Router,
  ) { }

  ngOnInit(): void {
    this.i18n.language$
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => {
        this.keyTranslation = createLocalizedFieldTranslation(this.i18n);
        this.cdr.markForCheck();
      });
  }

  performSearch() {
    let query = "";
    const fields = objKeys(this.fields)

    // if there's only one profile key directly navigate the user to the app view
    if (fields.length === 1 && fields[0] === 'profile' && this.fields.profile!.length === 1) {
      let profileName: string = this.fields.profile![0] || '';
      if (!profileName.includes("/")) {
        profileName = "local/" + profileName
      }
      this.router.navigate(['/app/' + profileName || ''])
      this.dialogRef.close();
      return;
    }

    fields.forEach(field => {
      this.fields[field]?.forEach(value => {
        query += `${field}:${JSON.stringify(value)} `
      })
    })

    if (query !== '' && this.textSearch !== '') {
      query += " "
    }
    query += this.textSearch;

    this.router.navigate(['/monitor'], {
      queryParams: {
        q: query,
      }
    })

    this.dialogRef.close();
  }

  onFieldsParsed(fields: SfngSearchbarFields) {
    objKeys(fields).forEach(field => {
      this.fields[field] = [...(this.fields[field] || []), ...(fields[field] || [])];
    })
  }
}
