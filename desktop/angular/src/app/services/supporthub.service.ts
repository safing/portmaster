/* eslint-disable @typescript-eslint/no-unused-vars -- the signatures stay as before for the callers, the arguments are no longer sent anywhere */
import { Injectable } from '@angular/core';
import { Observable, of, throwError } from 'rxjs';
import { environment } from 'src/environments/environment';

export interface SupportSection {
  title: string;
  body: string;
}

export interface Issue<CreatedAt = Date> {
  title: string;
  body: string;
  createdAt: CreatedAt;
  repository: string;
  url: string;
  user: string;
  closed?: boolean;
  labels: string[];
}

/**
 * Error returned for private support tickets, which were a service of Safing.
 */
export const ERR_TICKETS_REMOVED =
  'Private support tickets are not available in portmasterpro. Please open a GitHub issue instead.';

/**
 * SupportHubService used to talk to Safing's support hub:
 * it listed issues, uploaded debug information and created issues and
 * private tickets there.
 *
 * portmasterpro sends nothing to Safing. Issues are created by opening a
 * pre-filled "new issue" page of the project repository in the browser, so
 * the user sees and confirms everything that is submitted. Debug information
 * is never uploaded.
 */
@Injectable({ providedIn: 'root' })
export class SupportHubService {
  /** Issues are not fetched from any server. */
  loadIssues(): Observable<Issue[]> {
    return of([]);
  }

  /**
   * Debug information is not uploaded. Returns an empty URL, so that the
   * issue does not reference an upload.
   */
  uploadText(_name: string, _content: string): Observable<string> {
    return of('');
  }

  /**
   * Returns the URL of a pre-filled "new issue" page in the project
   * repository. Nothing is sent: the user opens the page and submits it.
   */
  createIssue(_repo: string, _preset: string, title: string, sections: SupportSection[], _debugInfoUrl?: string, _opts?: {
    generateUrl: boolean,
  }): Observable<string> {
    const body = [
      ...sections
        .filter(section => !!section.body?.trim())
        .map(section => `### ${section.title}\n\n${section.body.trim()}`),
      '_Debug information is not uploaded. If it helps, copy it from the support page and paste the relevant parts here._',
    ].join('\n\n');

    const params = new URLSearchParams({ title, body });
    return of(`${environment.issueTracker}/new?${params.toString()}`);
  }

  /** Private tickets were a support service of Safing and are not available. */
  createTicket(_repoName: string, _title: string, _email: string, _sections: SupportSection[], _debugInfoUrl?: string): Observable<void> {
    return throwError(() => new Error(ERR_TICKETS_REMOVED));
  }
}
