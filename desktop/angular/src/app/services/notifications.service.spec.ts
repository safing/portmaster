import { TestBed } from '@angular/core/testing';
import { WebsocketService } from '@safing/portmaster-api';
import { MockWebSocketSubject } from '@safing/portmaster-api/testing';
import { PartialObserver } from 'rxjs';
import { NotificationsService } from './notifications.service';
import { Notification, NotificationType } from './notifications.types';

describe('NotificationsService', () => {
  let service: NotificationsService;
  let mock: MockWebSocketSubject;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        {
          provide: WebsocketService,
          useValue: MockWebSocketSubject,
        }
      ]
    });
    service = TestBed.inject(NotificationsService);
    mock = MockWebSocketSubject.lastMock!;
  });

  afterEach(() => {
    mock.close();
  })

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should allow to query for notifications', () => {
    const observer = createSpyObserver();
    service.query("updates:").subscribe(observer);

    mock.expectLastMessage()
    mock.expectLastMessage('type').toBe('query')
    mock.expectLastMessage('query').toBe('notifications:all/updates:')

    mock.lastMultiplex!.next({
      id: mock.lastRequestId!,
      type: 'ok',
      data: {
        ID: 'updates:core-update-available',
        Message: 'Update available',
      },
      key: 'notifications:all/updates:core-update-available'
    })

    mock.lastMultiplex!.next({
      id: mock.lastRequestId!,
      type: 'ok',
      data: {
        ID: 'updates:ui-reload-required',
        Message: 'UI reload required',
      },
      key: 'notifications:all/updates:ui-reload-required'
    })

    // query collects all notifications using toArray
    // so nothing should be nexted yet.
    expect(observer.next).not.toHaveBeenCalled()
    expect(observer.error).not.toHaveBeenCalled()
    expect(observer.complete).not.toHaveBeenCalled()

    // finish the strea
    mock.lastMultiplex!.next({
      id: mock.lastRequestId!,
      type: 'done'
    })

    expect(observer.next).toHaveBeenCalledWith([
      {
        ID: 'updates:core-update-available',
        Message: 'Update available',
      },
      {
        ID: 'updates:ui-reload-required',
        Message: 'UI reload required',
      }
    ])
    expect(observer.error).not.toHaveBeenCalled()
    expect(observer.complete).toHaveBeenCalled()
  });

  describe('execute notification actions', () => {
    it('should work using a notif object', () => {
      let observer = createSpyObserver();
      let notif: any = {
        ID: 'updates:core-update-available',
        Message: 'An update is available',
        Type: NotificationType.Info,
        AvailableActions: [{ ID: "restart", Text: "Restart" }],
      }

      service.execute(notif, "restart").subscribe(observer);

      expect(observer.error).not.toHaveBeenCalled()

      mock.expectLastMessage('type').toBe('update');
      mock.expectLastMessage('key').toBe('notifications:all/updates:core-update-available');
      mock.expectLastMessage('data').toEqual({
        ID: 'updates:core-update-available',
        SelectedActionID: 'restart',
      });

      mock.lastMultiplex!.next({
        id: mock.lastRequestId!,
        type: 'success'
      })

      expect(observer.next).toHaveBeenCalledWith(undefined);
      expect(observer.error).not.toHaveBeenCalled();
      expect(observer.complete).toHaveBeenCalled();
    });

    it('should throw when executing an unknown action using a notif object', () => {
      let observer = createSpyObserver();
      let notif: any = {
        ID: 'updates:core-update-available',
        Message: 'An update is available',
        Type: NotificationType.Info,
        AvailableActions: [{ ID: "restart", Text: "Restart" }],
      }

      service.execute(notif, "restart-with-typo").subscribe(observer);

      expect(observer.error).toHaveBeenCalled()
      expect(mock.lastMessageSent).toBeUndefined();
    });

    it('should work using a key', () => {
      let observer = createSpyObserver();
      service.execute("updates:core-update-available", "restart").subscribe(observer);

      expect(observer.error).not.toHaveBeenCalled()

      mock.expectLastMessage('type').toBe('update');
      mock.expectLastMessage('key').toBe('notifications:all/updates:core-update-available');
      mock.expectLastMessage('data').toEqual({
        ID: 'updates:core-update-available',
        SelectedActionID: 'restart',
      });

      mock.lastMultiplex!.next({
        id: mock.lastRequestId!,
        type: 'success'
      })

      expect(observer.next).toHaveBeenCalledWith(undefined);
      expect(observer.error).not.toHaveBeenCalled();
      expect(observer.complete).toHaveBeenCalled();
    });
  })

  describe('resolving pending actions', () => {
    it('should work using a notif object', () => {
      let observer = createSpyObserver();
      let notif: any = {
        ID: 'updates:core-update-available',
        Message: 'An update is available',
        Type: NotificationType.Info,
        Responded: Math.round(Date.now() / 1000),
        SelectedActionID: "restart",
      }

      service.resolvePending(notif, 100).subscribe(observer)

      expect(observer.error).not.toHaveBeenCalled()

      mock.expectLastMessage('type').toBe('update');
      mock.expectLastMessage('key').toBe('notifications:all/updates:core-update-available');
      mock.expectLastMessage('data').toEqual({
        ID: 'updates:core-update-available',
        Executed: 100,
      });

      mock.lastMultiplex!.next({
        id: mock.lastRequestId!,
        type: 'success'
      })

      expect(observer.next).toHaveBeenCalledWith(undefined);
      expect(observer.error).not.toHaveBeenCalled();
      expect(observer.complete).toHaveBeenCalled();
    });

    it('should throw on an executed notification using a notif object', () => {
      let observer = createSpyObserver();
      let notif: any = {
        ID: 'updates:core-update-available',
        Message: 'An update is available',
        Type: NotificationType.Info,
        SelectedActionID: 'restart',
        Responded: Math.round(Date.now() / 1000),
        Executed: Math.round(Date.now() / 1000),
      }

      service.resolvePending(notif).subscribe(observer);

      expect(observer.error).toHaveBeenCalled()
      expect(mock.lastMessageSent).toBeUndefined();
    });

    it('should work using a key', () => {
      let observer = createSpyObserver();
      service.resolvePending("updates:core-update-available", 100).subscribe(observer);

      expect(observer.error).not.toHaveBeenCalled()

      mock.expectLastMessage('type').toBe('update');
      mock.expectLastMessage('key').toBe('notifications:all/updates:core-update-available');
      mock.expectLastMessage('data').toEqual({
        ID: 'updates:core-update-available',
        Executed: 100,
      });

      mock.lastMultiplex!.next({
        id: mock.lastRequestId!,
        type: 'success'
      })

      expect(observer.next).toHaveBeenCalledWith(undefined);
      expect(observer.error).not.toHaveBeenCalled();
      expect(observer.complete).toHaveBeenCalled();
    });
  });

  describe('watching notifications', () => {
    it('should be possible to watch for new and action-required notifs only', () => {
      const observer = createSpyObserver();
      service.new$.subscribe(observer);

      let send = (msg: any) => {
        mock.lastMultiplex!.next({
          id: mock.lastRequestId!,
          data: msg,
          type: 'ok',
          key: "notifications:all/" + msg.ID,
        })
      }

      let n1 = {
        ID: "new-notif-1",
        Message: "a new notification",
        Responded: 0,
        Executed: 0,
        Expires: Math.round(Date.now() / 1000) + 60 * 60,
      }
      let n2 = {
        ID: "new-notif-2",
        Message: "a new notification",
        Responded: 0,
        Executed: 0,
        Expires: 0,
        AvailableActions: [{ ID: "action-id", Text: "some action" }],
      }
      let expired = {
        ID: "new-notif-3",
        Message: "a new notification",
        Responded: 0,
        Executed: 0,
        Expires: 100,
      }
      let pending = {
        ID: "new-notif-4",
        Message: "a new notification",
        Responded: Math.round(Date.now() / 1000),
        Executed: 0,
        SelectedActionID: "test",
      }

      send(n1)
      send(expired)
      send(n2)
      send(pending)

      expect(observer.complete).not.toHaveBeenCalled()
      expect(observer.error).not.toHaveBeenCalled()
      expect(observer.next).toHaveBeenCalledTimes(2)
      expect(observer.next).toHaveBeenCalledWith(n1)
      expect(observer.next).toHaveBeenCalledWith(n2)
    })
  })

  describe('creating notifications', () => {
    it('should be possible using an object', () => {
      let notification: Partial<Notification<any>> = {
        ID: 'my-awesome-notification',
        AvailableActions: [
          { ID: 'action-no', Text: 'No' },
          { ID: 'force-no', Text: 'Hell No' }
        ],
        Message: 'Update complete, do you want to reboot?',
        Persistent: true,
        Type: NotificationType.Warning,
      }

      let observer = createSpyObserver();
      service.create(notification).subscribe(observer);

      expect(observer.error).not.toHaveBeenCalled();

      mock.expectLastMessage('type').toBe('create')
      mock.expectLastMessage('key').toBe('notifications:all/my-awesome-notification')
      mock.expectLastMessage('data').toEqual(notification);
      expect(notification.Created).toBeTruthy();

      mock.lastMultiplex!.next({
        type: 'success',
        id: mock.lastRequestId!,
      })

      expect(observer.complete).toHaveBeenCalled()
      expect(observer.error).not.toHaveBeenCalled()
      expect(observer.next).toHaveBeenCalledWith(undefined)
    })

    it('should be possible using parameters', () => {
      let observer = createSpyObserver();
      service.create('my-param-notification', 'message', NotificationType.Prompt, {
        Persistent: true,
        Created: 100,
      }).subscribe(observer);

      expect(observer.error).not.toHaveBeenCalled();

      mock.expectLastMessage('type').toBe('create')
      mock.expectLastMessage('key').toBe('notifications:all/my-param-notification')
      mock.expectLastMessage('data').toEqual({
        Type: NotificationType.Prompt,
        ID: 'my-param-notification',
        Message: 'message',
        Created: 100,
        Persistent: true,
      });

      mock.lastMultiplex!.next({
        type: 'success',
        id: mock.lastRequestId!,
      })

      expect(observer.complete).toHaveBeenCalled()
      expect(observer.error).not.toHaveBeenCalled()
      expect(observer.next).toHaveBeenCalledWith(undefined)

    })
  })
});

function createSpyObserver(): PartialObserver<any> {
  return jasmine.createSpyObj("observer", ["next", "error", "complete"])
}
