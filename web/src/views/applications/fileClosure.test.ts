import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { archiveName } from './archivePath';

const viewSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const applicationsApiSource = readFileSync(resolve(__dirname, '../../api/applications.ts'), 'utf8');
const facilityApiSource = readFileSync(resolve(__dirname, '../../api/facilityApps.ts'), 'utf8');
const managerSource = readFileSync(resolve(__dirname, '../../components/patterns/AssetFileManager.vue'), 'utf8');

describe('application and facility file closure', () => {
  it('uses one manager for application files and facility assets', () => {
    expect(viewSource.match(/<AssetFileManager/g)?.length).toBe(2);
    expect(viewSource).toContain(':adapter="applicationAssetAdapter"');
    expect(viewSource).toContain(':adapter="facilityAssetAdapter"');
    expect(viewSource).not.toContain('FacilityTextAssetDialog');
    expect(viewSource).not.toContain('openFacilityTextDialog');
    expect(managerSource).toContain('async function upload');
    expect(managerSource).toContain('async function replace');
    expect(managerSource).toContain('async function saveText');
    expect(managerSource).toContain('async function confirmDelete');
    expect(managerSource.match(/@click="openUpload"/g)?.length).toBe(1);
    expect(managerSource).toContain('uploadTypeText');
    expect(managerSource).toContain('binaryUploadKind');
  });

  it('derives a non-empty name for new archive uploads', () => {
    expect(archiveName('website.tar.gz')).toBe('website');
    expect(archiveName('assets.tgz')).toBe('assets');
    expect(archiveName('.zip')).toBe('archive');
    expect(viewSource).toContain('name: archiveName(file.name)');
  });

  it('uses scoped names for replacement and exposes both download states', () => {
    expect(viewSource).toContain('name: archiveName(file.name)');
    expect(applicationsApiSource).toContain('/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}/content');
    expect(applicationsApiSource).toContain('/applications/${id(applicationId)}/files/${id(fileName)}/content');
    expect(facilityApiSource).toContain('/edit-sessions/${id(sessionId)}/assets/${id(assetName)}/content');
    expect(facilityApiSource).toContain('/static-assets/${id(assetName)}/content');
  });

  it('only exposes facility text assets to the editor and saves the same name', () => {
    expect(viewSource).toContain("kind: kind === 'archive' ? 'uploaded_bundle' : 'uploaded_file'");
    expect(viewSource).toContain("contentMode: 'text'");
    expect(viewSource).toContain("contentMode: 'binary'");
    expect(viewSource).toContain('key: asset.name');
    expect(facilityApiSource).toContain("form.set('contentMode', input.contentMode ?? 'binary')");
  });

  it('uses asset names for facility route references', () => {
    expect(viewSource).toContain('v-model="facilityPathDraft.assetName"');
    expect(viewSource).not.toContain('v-model="facilityPathDraft.assetId"');
  });

  it('renders application and facility configuration as one continuous workspace', () => {
    expect(viewSource).toContain('class="app-editor-body"');
    expect(viewSource).toContain('class="workspace-panel"');
    expect(viewSource).toContain("t('applicationsPage.editorFlowHint')");
    expect(viewSource).toContain("t('applicationsPage.gatewayEditorFlowHint')");
    expect(viewSource).not.toContain("t('applicationsPage.editorWorkspace')");
    expect(viewSource).not.toContain("t('applicationsPage.gatewayWorkspace')");
    expect(viewSource).not.toContain('EditorSectionRail');
    expect(viewSource).not.toContain('activeAppSection');
    expect(viewSource).not.toContain('activeFacilitySection');
    expect(viewSource).not.toContain('v-show="');
    expect(viewSource).not.toContain(' 路 ');
  });
});
