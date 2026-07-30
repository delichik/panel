import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { archiveBasePath } from './archivePath';

const viewSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const applicationsApiSource = readFileSync(resolve(__dirname, '../../api/applications.ts'), 'utf8');
const facilityApiSource = readFileSync(resolve(__dirname, '../../api/facilityApps.ts'), 'utf8');

describe('application and facility file closure', () => {
  it('separates text editing from binary and archive uploads', () => {
    expect(viewSource).toContain("t('applicationsPage.newTextFile')");
    expect(viewSource).toContain("t('applicationsPage.uploadFile')");
    expect(viewSource).toContain("t('applicationsPage.uploadFolderArchive')");
    expect(viewSource).toContain('replaceApplicationFile(file, $event)');
    expect(viewSource).not.toContain('fileForm.kind');
    expect(viewSource).not.toContain('fileForm.contentType');
  });

  it('derives a non-empty folder path for new archive uploads', () => {
    expect(archiveBasePath('website.tar.gz')).toBe('website');
    expect(archiveBasePath('assets.tgz')).toBe('assets');
    expect(archiveBasePath('.zip')).toBe('archive');
    expect(viewSource).toContain('basePath: archiveBasePath(file.name)');
  });

  it('keeps stable keys for replacement and exposes both download states', () => {
    expect(viewSource).toContain('fileKey: targetKey');
    expect(viewSource).toContain('target?.assetKey');
    expect(applicationsApiSource).toContain('/application-edit-sessions/${id(sessionId)}/files/${id(fileKey)}/content');
    expect(applicationsApiSource).toContain('/applications/${id(applicationId)}/files/${id(fileId)}/content');
    expect(facilityApiSource).toContain('/edit-sessions/${id(sessionId)}/assets/${id(assetKey)}/content');
    expect(facilityApiSource).toContain('/static-assets/${id(assetId)}/content');
  });

  it('uses the shared top-step workspace for facility configuration', () => {
    expect(viewSource).toContain('v-for="section in facilityRailSections"');
    expect(viewSource).toContain("t('applicationsPage.gatewayWorkspace')");
    expect(viewSource).not.toContain('<EditorSectionRail');
  });
});
