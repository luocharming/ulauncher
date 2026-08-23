<script setup>

import {onMounted, reactive, ref, watch} from "vue";
import {
  ActivateLicenseCode,
  ControlRestartTask,
  GetAppConfig,
  GetLicenseInfo,
  GetVersionInfo,
  OpenFileDialog,
  UpdateSystemSettings,
  UploadLicenseFile
} from "@wails/go/api/systemApi";
import {Notify, copyToClipboard} from "quasar";

const systemSettings = reactive({
  RestartTaskCron: '',
  ExternalEditorPath: '',
  LogLevel: 0
})

const enableRestartTask = ref(false)
const versionInfo = ref({})
const licenseInfo = ref({
  applicationCode: '',
  valid: false,
  expireDate: '',
  remainingDays: 0,
  message: ''
})
const activationCode = ref('')

onMounted(async () => {
  const appConfig = await GetAppConfig();
  versionInfo.value = await GetVersionInfo();
  licenseInfo.value = await GetLicenseInfo();
  systemSettings.RestartTaskCron = appConfig.systemSettings.restartTaskCron
  systemSettings.LogLevel = appConfig.systemSettings.logLevel
  systemSettings.ExternalEditorPath = appConfig.systemSettings.externalEditorPath
  enableRestartTask.value = appConfig.systemSettings.enableRestartTask
  watch(systemSettings, async (value, oldValue, onCleanup) => {
    await UpdateSystemSettings(systemSettings)
  })
})

async function updateEnableRestartTask(value, ev) {
  try {
    await ControlRestartTask(value)
  } catch (err) {
    enableRestartTask.value = !value
    Notify.create("任务开启失败，" + err)
  }
}

function select() {
  OpenFileDialog("选择文件", "*.exe").then(result => {
    if (result) {
      systemSettings.ExternalEditorPath = result;
    } else {
      Notify.create({
        message: '文件选择取消'
      })
    }
  }).catch(err => {
    Notify.create({
      message: '文件选择出错,' + err
    })
  })
}

function copyAppCode() {
  copyToClipboard(licenseInfo.value.applicationCode).then(() => {
    Notify.create({message: '申请码已复制'})
  })
}

async function uploadLicense() {
  try {
    const result = await UploadLicenseFile()
    licenseInfo.value = result
    Notify.create({message: result.valid ? '授权文件上传成功' : result.message})
  } catch (e) {
    Notify.create({message: '上传失败：' + e})
  }
}

async function activateLicense() {
  if (!activationCode.value.trim()) {
    Notify.create({message: '请输入激活码'})
    return
  }
  try {
    const result = await ActivateLicenseCode(activationCode.value)
    licenseInfo.value = result
    if (result.valid) {
      activationCode.value = ''
      Notify.create({message: '激活成功'})
    } else {
      Notify.create({message: result.message})
    }
  } catch (e) {
    Notify.create({message: '激活失败：' + e})
  }
}

</script>
<template>
  <div class="text-h6">设置</div>
  <q-list>
    <q-item-label header>定时重启</q-item-label>
    <q-item dense>
      <q-item-section avatar>
        <q-toggle v-model="enableRestartTask" @update:model-value="updateEnableRestartTask"/>
      </q-item-section>
      <q-item-section>
        <q-item-label>开启定时重启任务</q-item-label>
        <q-item-label caption>定时重启本机实例提高UE长时间运行的稳定性</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-input :disable="enableRestartTask" dense v-model="systemSettings.RestartTaskCron" label="Cron (5位)"/>
      </q-item-section>
    </q-item>

    <q-item-label header>其他</q-item-label>
    <q-item dense>
      <q-item-section>
        <q-item-label>日志输出级别</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-select dense options-dense v-model="systemSettings.LogLevel"
                  :options="['debug','info','warn','error']"></q-select>
      </q-item-section>
    </q-item>
    <q-item dense>
      <q-item-section>
        <q-item-label>UE日志查看器路径(默认使用vscode)</q-item-label>
      </q-item-section>
      <q-item-section>
        <q-input dense v-model="systemSettings.ExternalEditorPath">
          <template v-slot:append>
            <q-btn padding="none" icon="sym_o_file_open" flat dense @click="select"/>
          </template>
        </q-input>
      </q-item-section>
    </q-item>
    <q-item-label header>授权管理</q-item-label>
    <q-item dense>
      <q-item-section>
        <q-item-label>申请码</q-item-label>
        <q-item-label caption style="word-break: break-all;">{{ licenseInfo.applicationCode }}</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-btn flat dense icon="sym_o_content_copy" @click="copyAppCode"/>
      </q-item-section>
    </q-item>
    <q-item dense>
      <q-item-section>
        <q-item-label>授权状态</q-item-label>
      </q-item-section>
      <q-item-section side>
        <div class="row items-center q-gutter-sm">
          <q-badge :color="licenseInfo.valid ? 'positive' : 'negative'"
                   :label="licenseInfo.valid ? '有效' : '未授权'"/>
          <span v-if="licenseInfo.valid" class="text-caption">
            到期：{{ licenseInfo.expireDate }} | 剩余：{{ licenseInfo.remainingDays }}天
          </span>
        </div>
      </q-item-section>
    </q-item>
    <q-item dense>
      <q-item-section>
        <q-input dense v-model="activationCode" placeholder="输入激活码，如 XXXX-XXXX-XXXX-XXXX">
          <template v-slot:append>
            <q-btn flat dense icon="sym_o_check_circle" color="primary" @click="activateLicense"/>
          </template>
        </q-input>
      </q-item-section>
      <q-item-section side>
        <q-btn flat dense color="primary" icon="sym_o_upload_file" label="上传授权文件" @click="uploadLicense"/>
      </q-item-section>
    </q-item>

    <q-item-label header>版本信息</q-item-label>
    <q-item dense>
      <q-item-section>
        <q-item-label>Version:</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-item-label>{{ versionInfo.Version }}</q-item-label>
      </q-item-section>
    </q-item>
    <q-item dense>
      <q-item-section>
        <q-item-label>BuildDate:</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-item-label>{{ versionInfo.BuildDate }}</q-item-label>
      </q-item-section>
    </q-item>
    <q-item dense>
      <q-item-section>
        <q-item-label>GitCommit:</q-item-label>
      </q-item-section>
      <q-item-section side>
        <q-item-label>{{ versionInfo.GitCommit }}</q-item-label>
      </q-item-section>
    </q-item>
  </q-list>
</template>
<style>
</style>