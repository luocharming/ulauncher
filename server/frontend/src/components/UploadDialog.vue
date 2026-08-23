<template>
  <q-dialog v-model="dialogVisible" persistent>
    <q-card style="min-width: 600px">
      <q-card-section>
        <div class="text-h6">上传模型</div>
      </q-card-section>

      <q-card-section class="q-pt-none">
        <!-- 文件上传区域 -->
        <q-uploader
          ref="uploaderRef"
          :factory="uploadFactory"
          multiple
          accept=".pak,.utoc,.ucas,.json,.png"
          :max-file-size="104857600"
          :max-files="10"
          label="拖拽文件到此处或点击选择"
          @added="handleFilesAdded"
          @removed="handleFileRemoved"
          @uploaded="handleUploadSuccess"
          @failed="handleUploadFailed"
          class="full-width"
        >
          <template v-slot:header="scope">
            <div class="row no-wrap items-center q-pa-sm q-gutter-xs">
              <q-btn
                v-if="scope.queuedFiles.length > 0"
                icon="cloud_upload"
                @click="scope.upload"
                round
                dense
                flat
              >
                <q-tooltip>开始上传</q-tooltip>
              </q-btn>
              <q-btn
                v-if="scope.uploadedFiles.length > 0"
                icon="done_all"
                @click="scope.removeUploadedFiles"
                round
                dense
                flat
              >
                <q-tooltip>清除已上传</q-tooltip>
              </q-btn>
              <q-btn
                v-if="scope.queuedFiles.length > 0"
                icon="clear_all"
                @click="scope.removeQueuedFiles"
                round
                dense
                flat
              >
                <q-tooltip>清除队列</q-tooltip>
              </q-btn>
              <q-spinner v-if="scope.isUploading" class="q-uploader__spinner" />
              <div class="col">
                <div class="q-uploader__title">上传文件</div>
                <div class="q-uploader__subtitle">
                  {{ scope.uploadSizeLabel }} / {{ scope.uploadProgressLabel }}
                </div>
              </div>
            </div>
          </template>

          <template v-slot:list="scope">
            <q-list separator>
              <q-item v-for="file in scope.files" :key="file.name">
                <q-item-section>
                  <q-item-label class="full-width ellipsis">
                    {{ file.name }}
                  </q-item-label>
                  <q-item-label caption>
                    {{ file.__sizeLabel }} / {{ file.__progressLabel }}
                  </q-item-label>
                </q-item-section>

                <q-item-section v-if="file.__img" thumbnail class="gt-xs">
                  <img :src="file.__img.src">
                </q-item-section>

                <q-item-section top side>
                  <q-btn
                    class="gt-xs"
                    size="12px"
                    flat
                    dense
                    round
                    icon="delete"
                    @click="scope.removeFile(file)"
                  />
                </q-item-section>
              </q-item>
            </q-list>
          </template>
        </q-uploader>

        <!-- 必需文件提示 -->
        <div class="q-mt-md text-caption text-grey-7">
          <div class="text-weight-bold q-mb-xs">必需文件：</div>
          <ul class="q-pl-md q-my-none">
            <li>metadata.json - 元数据文件</li>
            <li>asset.pak, asset.utoc, asset.ucas - UE资源文件</li>
            <li>thumbnail_64x64.png, thumbnail_256x256.png - 缩略图</li>
          </ul>
        </div>

        <!-- 上传进度 -->
        <q-linear-progress
          v-if="uploading"
          :value="uploadProgress / 100"
          color="primary"
          class="q-mt-md"
        />
      </q-card-section>

      <q-card-actions align="right">
        <q-btn flat label="取消" color="primary" @click="handleClose" :disable="uploading" />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import { useQuasar } from 'quasar';
import { uploadModel } from '@/model';

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
});

const emit = defineEmits(['update:modelValue', 'success']);

const $q = useQuasar();
const uploaderRef = ref(null);
const uploading = ref(false);
const uploadProgress = ref(0);

const dialogVisible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
});

// 文件上传工厂函数
const uploadFactory = (files) => {
  return new Promise((resolve, reject) => {
    const formData = new FormData();
    files.forEach(file => {
      formData.append('files', file);
    });

    uploading.value = true;
    uploadProgress.value = 0;

    uploadModel(formData, (progress) => {
      uploadProgress.value = progress;
    })
      .then(response => {
        if (response.code === 0) {
          $q.notify({
            type: 'positive',
            message: '上传成功',
            position: 'top'
          });
          emit('success');
          handleClose();
          resolve();
        } else {
          $q.notify({
            type: 'negative',
            message: '上传失败: ' + response.msg,
            position: 'top'
          });
          reject(new Error(response.msg));
        }
      })
      .catch(error => {
        $q.notify({
          type: 'negative',
          message: '上传失败: ' + error.message,
          position: 'top'
        });
        reject(error);
      })
      .finally(() => {
        uploading.value = false;
        uploadProgress.value = 0;
      });
  });
};

// 文件添加处理
const handleFilesAdded = (files) => {
  console.log('Files added:', files);
};

// 文件移除处理
const handleFileRemoved = (files) => {
  console.log('Files removed:', files);
};

// 上传成功处理
const handleUploadSuccess = (info) => {
  console.log('Upload success:', info);
};

// 上传失败处理
const handleUploadFailed = (info) => {
  console.log('Upload failed:', info);
};

// 关闭对话框
const handleClose = () => {
  if (!uploading.value) {
    dialogVisible.value = false;
    if (uploaderRef.value) {
      uploaderRef.value.reset();
    }
  }
};
</script>

<style scoped>
:deep(.q-uploader) {
  max-width: 100%;
}
</style>
