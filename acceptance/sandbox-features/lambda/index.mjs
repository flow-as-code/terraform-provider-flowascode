// A custom message processor that approves every chat message unchanged.
// Contract (event and response shapes):
// https://docs.aws.amazon.com/connect/latest/adminguide/redaction-message-processing.html
export const handler = async (event) => {
  const content = event?.chatContent ?? {};
  return {
    status: "APPROVED",
    result: {
      processedChatContent: {
        content: content.content ?? "",
        contentType: content.contentType ?? "text/plain",
      },
    },
  };
};
