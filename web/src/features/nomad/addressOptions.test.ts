import { buildNomadAddressOptions } from './addressOptions';

describe('buildNomadAddressOptions', () => {
  it('includes saved advertise, SSH host IP, and detected interface IPs without duplicates', () => {
    expect(
      buildNomadAddressOptions({
        host: '203.0.113.42',
        traits: {
          'nomad.advertise_address': '10.0.0.10',
          'sys.network_interfaces': 'eth0|inet|10.0.0.10/24, ens3|inet|10.0.0.12/24',
        },
      }),
    ).toEqual([
      { source: 'current', value: '10.0.0.10', name: undefined },
      { source: 'ssh', value: '203.0.113.42', name: undefined },
      { source: 'interface', value: '10.0.0.12', name: 'ens3' },
    ]);
  });

  it('uses the legacy advertise trait and ignores non-IP SSH hosts', () => {
    expect(
      buildNomadAddressOptions({
        host: 'example.com',
        traits: {
          'nomad.server_advertise_address': '10.0.0.20',
        },
      }),
    ).toEqual([{ source: 'current', value: '10.0.0.20', name: undefined }]);
  });
});
