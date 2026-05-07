// Mirrors api/quicktun/v1/*.proto JSON output.
// gRPC-gateway emits camelCase JSON keys regardless of proto snake_case.
// Extend this file as Task 2 wires up resource pages.

export type Operator = {
  name: string;
  operatorId: string;
  email: string;
  isAdmin: boolean;
  createTime?: string;
};

export type Project = {
  name: string;
  projectId: string;
  displayName: string;
  status: string;
  relayPortRange: string;
  createTime?: string;
};

export type Site = {
  name: string;
  siteId: string;
  displayName: string;
  status: string;
  lastSeenTime?: string;
  hostname?: string;
  mode: string;
};

export type Service = {
  name: string;
  serviceId: string;
  displayName: string;
  targetAddr: string;
  targetPort: number;
  proto: string;
  relayPort?: number;
};

export type StaleSite = {
  name: string;
  lastSeenAt?: string;
  status: string;
  hostname?: string;
};

export type SystemStatus = {
  operatorCount: number;
  projectCountActive: number;
  projectCountDisabled: number;
  siteCountOnline: number;
  siteCountOffline: number;
  siteCountPending: number;
  serviceCount: number;
  supervisorRunningCount: number;
  now: string;
  staleSites?: StaleSite[];
};

export type LoginResponse = {
  accessToken: string;
  expireTime?: string;
  operator: Operator;
};
