<template>
  <div class="model-detail">
    <q-inner-loading :showing="loading">
      <q-spinner-gears size="50px" color="primary" />
    </q-inner-loading>

    <div v-if="!loading && model" class="detail-container">
      <!-- 返回按钮 -->
      <q-btn
        flat
        icon="arrow_back"
        label="返回列表"
        @click="goBack"
        class="q-mb-md"
      />

      <div class="detail-content">
        <!-- 左侧：缩略图 -->
        <div class="left-panel">
          <q-img
            :src="model.thumbnail256"
            ratio="16/9"
            class="thumbnail"
          >
            <template v-slot:error>
              <div class="absolute-full flex flex-center bg-grey-3">
                <q-icon name="broken_image" size="80px" color="grey-5" />
              </div>
            </template>
          </q-img>
        </div>

        <!-- 右侧：详细信息 -->
        <div class="right-panel">
          <div class="text-h4 q-mb-md">{{ model.name }}</div>

          <q-list bordered separator>
            <q-item>
              <q-item-section>
                <q-item-label caption>UUID</q-item-label>
                <q-item-label>{{ model.uuid }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item>
              <q-item-section>
                <q-item-label caption>类型</q-item-label>
                <q-item-label>{{ model.type }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item>
              <q-item-section>
                <q-item-label caption>版本</q-item-label>
                <q-item-label>v{{ model.version }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item v-if="model.category">
              <q-item-section>
                <q-item-label caption>分类</q-item-label>
                <q-item-label>{{ model.category }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item>
              <q-item-section>
                <q-item-label caption>文件大小</q-item-label>
                <q-item-label>{{ formatFileSize(model.sizeBytes) }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item>
              <q-item-section>
                <q-item-label caption>创建时间</q-item-label>
                <q-item-label>{{ formatDateTime(model.createdTime) }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item>
              <q-item-section>
                <q-item-label caption>修改时间</q-item-label>
                <q-item-label>{{ formatDateTime(model.modifiedTime) }}</q-item-label>
              </q-item-section>
            </q-item>

            <q-item v-if="model.description">
              <q-item-section>
                <q-item-label caption>描述</q-item-label>
                <q-item-label>{{ model.description }}</q-item-label>
              </q-item-section>
            </q-item>
          </q-list>

          <!-- 操作按钮 -->
          <div class="action-buttons q-mt-md">
            <q-btn
              color="primary"
              icon="download"
              label="下载"
              @click="handleDownload"
              :loading="downloading"
            />
            <q-btn
              color="negative"
              icon="delete"
              label="删除"
              @click="handleDelete"
              class="q-ml-sm"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { useQuasar } from 'quasar';
import { getModelDetail, downloadModel, deleteModel, getThumbnailUrl, formatFileSize, formatDateTime } from '@/model';

const props = defineProps({
  uuid: {
    type: String,
    required: true
  }
});

const emit = defineEmits(['back']);
const $q = useQuasar();

const model = ref(null);
const loading = ref(false);
const downloading = ref(false);

// 加载模型详情
const loadModelDetail = async () => {
  loading.value = true;
  try {
    const response = await getModelDetail(props.uuid);

    // 使用宽松比较，兼容字符串和数字类型的 code
    if (response.code == 200) {
      model.value = response.data;
      model.value.thumbnail256 = getThumbnailUrl(model.value.uuid, '256x256', model.value.version);
    } else {
      $q.notify({
        type: 'negative',
        message: '加载模型详情失败: ' + response.msg,
        position: 'top'
      });
    }
  } catch (error) {
    $q.notify({
      type: 'negative',
      message: '加载模型详情失败: ' + error.message,
      position: 'top'
    });
  } finally {
    loading.value = false;
  }
};

// 返回列表
const goBack = () => {
  emit('back');
};

// 下载模型
const handleDownload = async () => {
  downloading.value = true;
  try {
    const response = await downloadModel(model.value.uuid, model.value.version);

    // 创建下载链接
    // response.data 包含 blob 数据
    const url = window.URL.createObjectURL(new Blob([response.data]));
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', `${model.value.uuid}_v${model.value.version}.zip`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.URL.revokeObjectURL(url);

    $q.notify({
      type: 'positive',
      message: '下载成功',
      position: 'top'
    });
  } catch (error) {
    $q.notify({
      type: 'negative',
      message: '下载失败: ' + error.message,
      position: 'top'
    });
  } finally {
    downloading.value = false;
  }
};

// 删除模型
const handleDelete = () => {
  $q.dialog({
    title: '确认删除',
    message: `确定要删除模型 "${model.value.name}" 吗？此操作不可恢复。`,
    cancel: true,
    persistent: true
  }).onOk(async () => {
    try {
      const response = await deleteModel(model.value.uuid, model.value.version);

      // 使用宽松比较，兼容字符串和数字类型的 code
      if (response.code == 200) {
        $q.notify({
          type: 'positive',
          message: '删除成功',
          position: 'top'
        });
        emit('back');
      } else {
        $q.notify({
          type: 'negative',
          message: '删除失败: ' + response.msg,
          position: 'top'
        });
      }
    } catch (error) {
      $q.notify({
        type: 'negative',
        message: '删除失败: ' + error.message,
        position: 'top'
      });
    }
  });
};

// 页面加载时获取数据
onMounted(() => {
  loadModelDetail();
});
</script>

<style scoped>
.model-detail {
  width: 100%;
  height: 100%;
  padding: 20px;
  position: relative;
}

.detail-container {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.detail-content {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 30px;
  flex: 1;
  overflow-y: auto;
}

.left-panel {
  display: flex;
  flex-direction: column;
}

.thumbnail {
  width: 100%;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.right-panel {
  display: flex;
  flex-direction: column;
}

.action-buttons {
  display: flex;
  gap: 10px;
}

@media screen and (max-width: 1366px) {
  .detail-content {
    grid-template-columns: 1fr;
  }
}
</style>
