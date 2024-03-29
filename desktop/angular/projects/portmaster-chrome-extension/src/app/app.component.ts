import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit } from '@angular/core';
import { NavigationEnd, Router } from '@angular/router';
import { MetaAPI, MyProfileResponse, retryPipeline } from '@safing/portmaster-api';
import { catchError, filter, throwError } from 'rxjs';


@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss'],
})
export class AppComponent implements OnInit {
  isAuthorizeView = false;

  constructor(
    private metaapi: MetaAPI,
    private router: Router,
  ) { }

  profile: MyProfileResponse | null = null;

  ngOnInit(): void {
    this.router.events
      .pipe(
        filter(event => event instanceof NavigationEnd)
      )
      .subscribe(event => {
        if (event instanceof NavigationEnd) {
          this.isAuthorizeView = event.url.includes("/authorize")
        }
      })

    this.metaapi.myProfile()
      .pipe(
        catchError(err => {
          if (err instanceof HttpErrorResponse && err.status === 403) {
            this.router.navigate(['/authorize'])
          }

          return throwError(() => err)
        }),
        retryPipeline()
      )
      .subscribe({
        next: profile => {
          this.profile = profile;

          console.log(this.profile);
        }
      })
  }

}
