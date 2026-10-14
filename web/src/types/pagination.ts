export interface ListPage<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}
