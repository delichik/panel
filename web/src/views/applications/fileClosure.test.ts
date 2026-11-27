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
    expect(viewSource).toContain("const name = kind === 'archive' ? archiveName(file.name) : file.name;");
  });

  it('uses scoped names for replacement and exposes both download states', () => {
    expect(viewSource).toContain("const name = kind === 'archive' ? archiveName(file.name) : file.name;");
    expect(applicationsApiSource).toContain('/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}/content');
    expect(applicationsApiSource).toContain('/applications/${id(applicationId)}/files/${id(fileName)}/content');
    expect(facilityApiSource).toContain('/edit-sessions/${id(sessionId)}/assets/${id(assetName)}/content');
    expect(facilityApiSource).toContain('/static-assets/${id(assetName)}/content');
  });

  it('only exposes facility text assets to the editor and saves the same name', () => {
    expect(viewSource).toContain("const uploadKind = item.kind === 'archive' ? 'uploaded_bundle' : 'uploaded_file';");
    expect(viewSource).toContain("const contentMode = item.kind === 'text' ? 'text' : 'binary';");
    expect(viewSource).toContain('key: asset.name');
    expect(facilityApiSource).toContain("form.set('contentMode', input.contentMode ?? 'binary')");
  });

  it('uses asset names for facility route references', () => {
    expect(viewSource).toContain('v-model="facilityPathDraft.assetName"');
    expect(viewSource).not.toContain('v-model="facilityPathDraft.assetId"');
  });

  it('selects application file mount sources from the active edit session', () => {
    expect(viewSource).toContain(':options="applicationFileMountOptions"');
    expect(viewSource).toContain("mountDraft.type === 'file'");
    expect(viewSource).toContain("t('applicationsPage.applicationFileMountMissing'");
  });

  it('selects Seamark file mount sources from the shared template catalog', () => {
    expect(viewSource).toContain('panelFiles.value = catalog.panelFiles ?? []');
    expect(viewSource).toContain(':options="panelFileMountOptions"');
    expect(viewSource).toContain("mountDraft.type === 'panel_file'");
    expect(viewSource).toContain("t('applicationsPage.panelFileMountMissing'");
  });

  it('renders application and facility configuration as one continuous workspace', () => {
    // 编辑器只保留一层页面 padding / 一层滚动：ConsolePage 关闭自身滚动，EditorPage 是唯一滚动 owner。
    expect(viewSource).toContain(':body-scroll="false"');
    expect(viewSource).toContain('<EditorPage>');
    expect(viewSource).not.toContain('class="app-editor-body"');
    expect(viewSource).not.toContain('class="app-editor-shell"');
    expect(viewSource).not.toContain('class="app-editor-header"');
    expect(viewSource).not.toContain('<style');
    expect(viewSource).toContain("t('applicationsPage.editorFlowHint')");
    expect(viewSource).toContain("t('applicationsPage.gatewayEditorFlowHint')");
    expect(viewSource).toContain('<DetailSection :title="t(\'applicationsPage.panelIdentity\')"');
    expect(viewSource).not.toContain("t('applicationsPage.editorWorkspace')");
    expect(viewSource).not.toContain("t('applicationsPage.gatewayWorkspace')");
    expect(viewSource).not.toContain('EditorSectionRail');
    expect(viewSource).not.toContain('activeAppSection');
    expect(viewSource).not.toContain('activeFacilitySection');
    expect(viewSource).not.toContain('v-show="');
    expect(viewSource).not.toContain(' 路 ');
  });

  it('separates networking, container environment, and storage into peer panels', () => {
    const networkingPanel = viewSource.indexOf("<DetailSection :title=\"t('applicationsPage.panelNetworking')\"");
    const environmentPanel = viewSource.indexOf("<DetailSection :title=\"t('applicationsPage.containerEnv')\"");
    const storagePanel = viewSource.indexOf("<DetailSection :title=\"t('applicationsPage.panelStorage')\"");

    expect(networkingPanel).toBeGreaterThan(-1);
    expect(environmentPanel).toBeGreaterThan(networkingPanel);
    expect(storagePanel).toBeGreaterThan(environmentPanel);
    expect(viewSource).toContain('v-if="!appDraft.env.length"');
    expect(viewSource).toContain('v-if="!appDraft.mounts.length"');
    expect(viewSource).not.toContain('v-if="!appDraft.env.length && !appDraft.mounts.length"');
    // 共享 pattern 取代手写分区排版；检查结果改为保存失败时弹窗复用诊断列表。
    expect(viewSource).toContain('<FormField');
    expect(viewSource).toContain('<ApplicationDiagnosticsList');
    expect(viewSource).toContain('<InfoBar');
  });
});
