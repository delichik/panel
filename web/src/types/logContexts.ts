export type {
  Event as LogContext,
  EventPage as LogContextPage,
  LogLine as LogContextLine,
  LogLinesPage as LogContextLinesPage,
  EventQuery as LogContextQuery,
} from './events';

/** @deprecated Use Event actor/resources were removed from the product DTO. */
export interface LogContextActor {
  kind?: string;
  id?: string;
  name?: string;
}

/** @deprecated Use Event actor/resources were removed from the product DTO. */
export interface LogContextResource {
  resourceType: string;
  resourceId: string;
  nameSnapshot?: string;
  role?: string;
}
