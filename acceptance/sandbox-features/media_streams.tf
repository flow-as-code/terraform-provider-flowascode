# D02: live media streaming with no data retention, for the
# UpdateContactMediaStreamingBehavior probes. "No data retention" is
# retention_period_hours = 0: the audio is readable for five minutes and then
# gone. The key is the AWS managed key for Kinesis Video Streams, which the
# admin guide allows beside a customer managed one.
# https://docs.aws.amazon.com/connect/latest/adminguide/enable-live-media-streams.html
#
# Cost: Kinesis Video Streams charges $0.0085 per GB ingested and nothing
# idle; with no retention there is no storage charge.
# https://aws.amazon.com/kinesis/video-streams/pricing/

resource "aws_connect_instance_storage_config" "media_streams" {
  instance_id   = var.instance_id
  resource_type = "MEDIA_STREAMS"

  storage_config {
    storage_type = "KINESIS_VIDEO_STREAM"

    kinesis_video_stream_config {
      prefix                 = "${local.prefix}-media"
      retention_period_hours = 0

      encryption_config {
        encryption_type = "KMS"
        key_id          = data.aws_kms_key.kinesisvideo.arn
      }
    }
  }
}
