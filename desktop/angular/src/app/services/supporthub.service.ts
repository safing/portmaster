import { HttpClient, HttpParams } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
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

@Injectable({ providedIn: 'root' })
export class SupportHubService {
  constructor(private http: HttpClient) { }

  loadIssues(): Observable<Issue[]> {
    interface LoadIssuesResponse {
      issues: Issue<string>[];
    }
    return this.http.get<LoadIssuesResponse>(`${environment.supportHub}/api/v1/issues`)
      .pipe(map(res => res.issues.map(issue => ({
        ...issue,
        createdAt: new Date(issue.createdAt),
      })).reverse()));
  }

  /** Uploads content under name */
  uploadText(name: string, content: string): Observable<string> {
    interface UploadResponse {
      urls: {
        [key: string]: string[];
      }
    }
    const blob = new Blob([content], { type: 'text/plain' });
    const data = new FormData();
    data.set("file", blob, name);

    return this.http.post<UploadResponse>(`${environment.supportHub}/api/v1/upload`, data)
      .pipe(map(res => res.urls['file'][0]));
  }

  /** Create github issue */
  createIssue(repo: string, preset: string, title: string, sections: SupportSection[], debugInfoUrl?: string, opts?: {
    generateUrl: boolean,
  }): Observable<string> {
    interface CreateIssueResponse {
      url: string;
    }
    const req = {
      title,
      sections,
      debugInfoUrl
    }
    let params = new HttpParams();
    if (!!opts?.generateUrl) {
      params = params.set('generate-url', '')
    }
    return this.http.post<CreateIssueResponse>(`${environment.supportHub}/api/v1/issues/${repo}/${preset}`, req, { params }).pipe(map(r => r.url))
  }

  createTicket(repoName: string, title: string, email: string, sections: SupportSection[], debugInfoUrl?: string): Observable<void> {
    const req = {
      title,
      sections,
      debugInfoUrl,
      email,
      repoName,
    }
    return this.http.post<void>(`${environment.supportHub}/api/v1/ticket`, req)
  }
}
